# LGT implementation history

This file preserves implementation investigations and dated validation records.
Statements about current behavior, missing APIs, corpus size, and planned work
apply to their original revision; later records may supersede them.
Start with [lgt.md](../lgt.md) for the maintained overview.
This consolidation adds no execution or acceptance evidence.


## Contents

- [Implementation record through 2026-09-17](#implementation-the-lgt-platform)
- [What an LGT title is](#implementation-what-an-lgt-title-is)
- [Loading](#implementation-loading)
- [A title draws past the surface it was given, so pixels live apart](#implementation-a-title-draws-past-the-surface-it-was-given-so-pixels-live-apart)
- [Startup](#implementation-startup)
- [The import tables](#implementation-the-import-tables)
- [The WIPI C slots](#implementation-the-wipi-c-slots)
- [The two arc slots come from their neighbours](#implementation-the-two-arc-slots-come-from-their-neighbours)
- [Drawing: the context belongs to the game](#implementation-drawing-the-context-belongs-to-the-game)
- [Timers](#implementation-timers)
- [Input](#implementation-input)
- [The guest clock advances with work, not only with ticks](#implementation-the-guest-clock-advances-with-work-not-only-with-ticks)
- [Files](#implementation-files)
- [Sound](#implementation-sound)
- [Images](#implementation-images)
- [The Java interface table](#implementation-the-java-interface-table)
- [What a sweep of a 94-archive set asked for](#implementation-what-a-sweep-of-a-94-archive-set-asked-for)
- [Reclaiming what a Java title stopped holding](#implementation-reclaiming-what-a-java-title-stopped-holding)
- [Deliberately incomplete](#implementation-deliberately-incomplete)
- [A transparent pixel still has a colour](#implementation-a-transparent-pixel-still-has-a-colour)
- [Guest profiling, and what it says about where the time goes](#implementation-guest-profiling-and-what-it-says-about-where-the-time-goes)
- [Finding out what a title was told](#implementation-finding-out-what-a-title-was-told)
- [The Clet is told when its page goes away](#implementation-the-clet-is-told-when-its-page-goes-away)
- [Playing a title from the command line](#implementation-playing-a-title-from-the-command-line)
- [Testing](#implementation-testing)
- [Cheats](#implementation-cheats)
- [Startup after an existing save (2026-09-12)](#implementation-startup-after-an-existing-save-2026-09-12)
- [Local binary authentication confirmation (2026-09-13)](#implementation-local-binary-authentication-confirmation-2026-09-13)
- [Compatibility follow-up (2026-09-12)](#followup-lgt-compatibility-follow-up)
- [Java API coverage follows dispatch](#followup-java-api-coverage-follows-dispatch)
- [What the import scan proves](#followup-what-the-import-scan-proves)
- [Guest stream mark and reset](#followup-guest-stream-mark-and-reset)
- [Save and menu evidence](#followup-save-and-menu-evidence)
- [Screen orientation evidence](#followup-screen-orientation-evidence)
- [Native text input boundary](#followup-native-text-input-boundary)
- [LGT rendering, input and pacing QA — 2026-09-20](#lgt-qa-2026-09-20)
- [LGT startup, rendering, and media QA — 2026-09-21](#lgt-qa-2026-09-21)
- [LGT key callback hang — 2026-09-23](#lgt-key-callback-hang-2026-09-23)
- [Newly added archives — 2026-10-19](#lgt-new-archives-2026-10-19)
- [A name field the key rule refused — 2026-10-20](#lgt-text-input-2026-10-20)
- [Text input review follow-ups — 2026-10-20](#lgt-text-input-review-2026-10-20)
- [Image slots, C library names and the storage contract — 2026-10-22](#lgt-slot-corrections-2026-10-22)

<a id="implementation-the-lgt-platform"></a>

## Implementation record through 2026-09-17

For the current post-#143 API gaps, scanner limitations, and dated validation,
see [the 2026-09-12 coverage audit](coverage.md#lgt-lgt-coverage-audit-after-pr-143). Historical corpus
counts and findings below describe their own runs rather than today's coverage.

The network socket-creation API now accepts both observed slots, `0x7d0` and
the standard-order `0x25a`. Both refuse sockets normally. The latter was verified
inside a successful-dial callback; it is not inferred solely from slot order.
See [callback evidence and offline-policy limits](../authentication.md#lgt-socket-prerequisite).

LGT (LG Telecom) shipped its own WIPI implementation. Three differences from
KTF determine the rest:

| | KTF | LGT |
|---|---|---|
| Binary format | raw ARM image (`client.bin`) | **ELF** (`binary.mod`) |
| Function binding | direct callback pointers | **import table lookup** |
| C standard library | linked into the game | **provided by the platform** |

The ARM core, the save boundary, the glyph font and the archive handling are
shared with the rest of the runtime; only the loader and the platform surface
are new.

Every non-Java title in the local set boots, draws, plays sound, takes input,
and has been driven from its title screen through its menus into the world,
including those that authenticate over an absent network, because a refused
connection is a path they already handle. The record below comes from those
runs; several contracts are corrections only a real title exposed.

<a id="implementation-what-an-lgt-title-is"></a>

### What an LGT title is

A zip holding `app_info` — a flat `key=value` descriptor naming the AID, PID
and MClass — beside the game's JAR. The JAR holds `binary.mod` (the ELF) and
the game's resources.

The JAR is normally named after the AID, but repacked archives keep the
original JAR next to an edited `app_info`, so **the JAR that is present wins**
when the AID-named one is missing.

`SaveOwner` is the PID, with the AID as a fallback, because the handset's save
layout is keyed by PID.

**PIDs are not reliably unique.** Two local titles carry the same PID with
different AIDs and resolve to the same save directory. No archive shows whether
that is correct: if the handset collided too, a shared store is what it did; if
a repack wrote the wrong `app_info`, the identity is wrong. Nothing was changed
on a guess and the PID still wins. The symptom would be one title reading
another's save; the AID is the discriminator if the two must be split.

<a id="implementation-loading"></a>

### Loading

`ParseModule` reads ELF32 little-endian ARM executables and nothing else:
a wrong class, endianness, type or machine fails at the header rather than as
a wild jump later. Sections with a nonzero address are loaded at that address;
`SHT_NOBITS` (`.bss`) occupies space without file bytes.

**One mapping spans the module** rather than one per section. ELF sections are
page-adjacent; per-section mappings would leave the gaps unmapped, and a module
that addresses its own padding would fault there.

There is no ELF library: the project takes no new dependencies, and the parts
of ELF that would justify one are parts this loader never touches.

<a id="implementation-a-title-draws-past-the-surface-it-was-given-so-pixels-live-apart"></a>

### A title draws past the surface it was given, so pixels live apart

The platform's memory is two regions rather than one. `platformDataBase` holds
what the platform writes for the module — the init parameter blocks, the
surface records, the copies of resource names it hands out as ids — and
`surfaceBase`, sixteen megabytes further up, holds nothing but pixels: the LCD
and every offscreen surface a title draws into.

**A Clet's drawing is not bounded by the surface it was given.** It writes
pixels through the pointer this platform hands it, and one local title's
pre-first-frame clear loop walks several thousand bytes past the end of the
LCD. On a handset that lands in whatever follows the panel's memory. Here it
landed in the next arena allocation, a copy of the resource name
`MC_knlGetResourceID` had answered with, sixty bytes past the end of the screen.

The cost was not a wrong pixel. The name read back empty, so the following
`MC_knlGetResource` looked for a resource called "" and failed; the title took
the size out of the unfilled buffer, asked `MC_knlAlloc` for 0x1c1c1c1c bytes,
got the refusing null, and stored through it. **The report was a write to
address zero inside a timer callback, seven hundred instructions and one
platform call after the cause.** A second title died the same way at a
different address, because what the overrun destroys depends on what the arena
had put there.

Both of two fixes are needed. Pixels are allocated from their own region, so an
overrun lands in space nothing else keeps; and **an id this platform issued is
read back from its own table rather than from guest memory** (`resourceNames`
in `client.go`), because a handle whose meaning lives where the guest can write
it is a handle the guest can destroy. The id stays a pointer to the name, which
is what the other WIPI platform hands out and what a title stores as the
resource's identity, but the platform no longer depends on those bytes.

**A whole-set A/B shows the split costs nothing.** The 94 local archives run
for three thousand ticks before and after are identical, and the two titles
that died now run their reported sessions to the end (one only on its second
run, because its first writes a save and ends itself).

**A watchpoint on the platform's own bytes found it.** A probe comparing every
issued id against its name after every supervisor call named the moment the
copy changed; a write watch on that address named the instruction. Both are in
`armcore`. The watch reports guest and host writers separately, which showed
the store was the title's own.

<a id="implementation-startup"></a>

### Startup

1. The platform hands the entry point two blocks. The first is scratch the
   module fills in with a pointer to its init struct; the second carries two
   callbacks: **get import table** and **get import function**.

   **The entry point is entered in Thumb**, though the ELF does not say so:
   every archive here has an even `e_entry` pointing at Thumb code. Taking the
   low bit at face value runs the module in ARM, where halfword pairs decode as
   mostly skipped conditional instructions, so the fault comes a few
   instructions later, on whichever one the flags let through, reading an
   address that was never a pointer.
2. The module resolves every platform function it needs through those, at
   startup, before it does anything else.
3. The platform reads `param1->ptr_init_struct`, whose second word is the
   module's initializer, and calls it.
4. The initializer calls `MC_cletRegister` with a table of six entry points:
   start, pause, resume, destroy, paint, handleEvent. **The runtime calls into
   the game only through those.**

`Session` drives the result: `Tick` advances the guest clock, fires timers,
delivers queued events, and asks for a paint.

<a id="implementation-the-import-tables"></a>

### The import tables

| Table | Contents |
|---|---|
| `0x1fb` | WIPI C functions |
| `0x1` | C standard library |
| `0x64` | Java interface — resolves, but no Java app runs; see below |
| `0x1f8` | LGT's own. Contents unknown; see below |

An unknown table is an **error**, not a null pointer: a module handed zero
branches to zero and fails far from the cause. A *known table with an
unimplemented slot* still gets a stub, because a module resolves everything it
might use at startup and refusing there would stop a game over a function it
never calls; reaching the slot reports the gap.

`0x1f8` shows why. Every non-Java archive here resolves slot `0x16` first thing
in its initializer, and refusing the table stopped all nine titles before they
had registered anything. Nothing names the table: not the specification, not
the original runtime's symbols, not the reference implementation, which knows
the same two slots and calls them unknown.

Slot `0x16` takes four arguments and every caller discards its result. The
calls pass `(0, 0x64, 240, 0)` and `(0, 0x65, 320, 0)`: small keys against
this platform's LCD width and height, which reads like the module declaring the
screen it was built for. **Nothing acts on that reading**, because the values
already match the LCD, some modules make a third call the reading does not
explain, and resizing the screen on a wrong guess would break every title.
Accept-and-ignore is known to be right; `internal/platform/lgt/oem.go` records
the evidence.

<a id="implementation-the-imports-a-module-resolves-are-its-link-map"></a>

#### The imports a module resolves are its link map

Nothing in an LGT archive lists what a title links against. The ELF carries no
dynamic symbols, a platform function is a pair of numbers rather than a name,
and those numbers appear only in the resolution calls the module makes at
startup, for everything it might call, before running any of its own code.

The platform therefore records each `(category, slot)` a module resolves once
(`internal/platform/lgt/imports.go`); `ResolvedImports` reports them with the
platform's name for each and whether reaching it would be serviced.
`internal/tools/apiscan` starts every archive in a directory and prints the
unserviced ones, ranked by how many titles resolve each. It boots each archive
rather than reading it as the other two platforms' scans do, because nothing
cheaper is also exact. (The stub-record scan under "Reading a module without
running it" recovers the same list statically, but needs a disassembler this
repository does not carry.)

Local corpus result: **the 25 Clet titles resolve nothing this platform does
not implement**, so the C surface is complete for everything they link against,
a stronger statement than "they run". Every gap belongs to the four Java
titles: three auxiliary Java tables (`0x1fc`, `0x1ff`, `0x201`, index 3 of
each), the OEM table's Java entry `0x17`, and C library slot `0x32`, which sits
far below the C library's base and is resolved by no Clet.

The scan also found a naming hole: `0x424`, the slot that runs a function, is
serviced but was missing from `stdlibSlotNames`, so its trace lines read
`unnamed` and the scan counted it as unimplemented. The table is the platform's
record of what it services, so a hole there is a hole in both readings.

<a id="implementation-the-wipi-c-slots"></a>

### The WIPI C slots

They are a flat index in blocks of 100 (`0x64`). Four blocks are identified,
each confirmed by the functions inside it rather than by arithmetic: `0x64`
kernel, `0xc8` graphics, `0x190` filesystem, `0x258` network. The C library is
a separate table (`0x1`) with its base at `0x3e8`.

**Within a block the slots follow the specification's function order**, and
this was checked: the kernel block's `0x64` is its first function, `0x6a` its
seventh, and `0x75` through `0x81` its eighteenth through thirtieth, fourteen
consecutive slots exactly where the specification lists them. The filesystem
block agrees (open, read, write, close, seek, attribute, remove … `0x1a0` is the
seventeenth, `MC_fsIsExist`), and so does the network block, whose imported set
(connect, close, socket connect/write/read/close, the two callback setters) is a
socket client's needs at the specification's offsets. That is enough to read an
unidentified slot's number as a name **inside an identified block**.

**It is not enough to identify a block.** The blocks past the filesystem do not
follow the specification's section order: media sits at `0x4b0`, where the
sections would put the C library. The arithmetic that made block three the
record database was wrong: block three is the input-method block, and no title
here imports a database call. "Character input" below records how that was
settled; `internal/platform/lgt/wipic_unknown.go` records slots answered
without being understood, and is currently empty.

**Everything below `0x64` is LGT's own OEM block**, holding the framebuffer
accessors: `0x32` pointer, `0x33` width, `0x34` height, `0x35` bytes-per-line,
`0x36` bpp. Some titles render nearly everything through them (sprites,
tilemaps, their own Korean bitmap font), so these five matter far more than
their call count suggests. `0x36` **takes no argument**, because it reports the
depth of the LCD rather than of a framebuffer.

These contracts looked right until a dependent call arrived, and each wrong one
ended as a null the game walked into:

- **`0x80` / `0x81` are a pair.** `MC_knlGetResourceID` turns a name into an id
  and reports the size; `MC_knlGetResource` reads *that id*. Answering the size
  from the first call satisfies a title that only wants the size and hands the
  second call a length where it expects a name. The id is a guest pointer to a
  copy of the name, the same handle KTF hands out.
- **One name answers one id** for the life of the client, because the id is the
  resource's *identity* to a title. One title reads its resource list at boot (a
  packaged file of names and a second of sizes), builds an `{id, size}` table,
  then loads a resource by asking for its id again and searching the table. A
  fresh copy of the name per call makes every search miss. The miss leaves the
  caller's size at its initial zero, so the title allocates nothing, reads its
  record count from whatever heap follows the empty buffer, sizes a table from
  that, and walks off its end. Its loop counter is a `signed char`, so a count
  above 127 wraps to -128 and the writes land *below* the heap. The title died
  several thousand platform calls after the cause; the ring buffer at the fault
  showed only a loop allocating one byte at a time for empty names.
- **`0x81` answers zero, not the byte count.** Callers are written as
  `id = getResourceID(name, &size); buf = calloc(size); if (getResource(id, buf,
  size)) { free(buf); return 0; }`, so nonzero is the *failure* branch.
  Reporting the length freed the just-filled buffer and handed the caller a null
  it copied from a few instructions later; three titles died that way. The one
  documented failure is a buffer too small for the resource, `M_E_SHORTBUF`.
- **`0x76` takes one size.** `MC_knlCalloc` is "allocate and zero", not C's
  `(count, size)` allocator. Multiplying in a second argument that is really the
  caller's stack pointer turns a 33-byte request into one no arena can serve.
- **`0x7e` answers.** Titles read `PHONEMODEL` and `PHONENUMBER` to choose a
  path. The answers are shared with KTF in `internal/wipic`, because the
  question is about the handset rather than the runtime.
- **`localtime` returns a struct.** It used to answer null, since laying out a
  `tm` for a specific libc was a guess no title had asked for. One title calls
  `localtime` and loads offset 4 straight off the result. The specification
  lists `localtime` among the "supported ANSI-C interface functions" without
  restating its interface, so ANSI C's contract applies: nine ints in
  declaration order, in storage the caller does not own and must not free.

<a id="implementation-mc_knlexit-sits-at-0x68"></a>

#### `MC_knlExit` sits at `0x68`

It had been `0x6b`, which the kernel numbering makes
`MC_knlGetParentProgramID`: a title asking who launched it would have ended its
own session, and one asking to exit would have hit a missing slot. No local
title reaches either; the correction came from the numbering, not a failure.

<a id="implementation-mc_knlsprintk-renders-a-real-printf-precision-included"></a>

#### `MC_knlSprintk` renders a real printf, precision included

The renderer is shared with the other WIPI platform in `internal/wipic`,
because a format string means the same thing wherever the call came from. It
accepts `%%`, `%c`, `%s`, `%d`, `%i`, `%u`, `%o`, `%x`, `%X` and `%p`, the `-`,
`0`, `+`, space and `#` flags, a width, a precision, and the `h`/`l` length
modifiers. Width and precision may each be `*`, which reads its own argument
word.

**A conversion the subset cannot parse is emitted as written**, so a missing
one is visible; that is how the precision came to be implemented. One title's
dialogue is a single block of text with each line stored as a byte count into
it, drawn with `%.*s`; with `.` unrecognised, **every line of dialogue read
`%.*s`**. That title also uses `%s:%.*s` and `%.3s-****-%.4s`.

The precision bounds the read. `%.*s` prints a slice of a buffer that need not
be terminated, so the guest read is counted and stops at a terminator without
requiring one; requiring one reads off the end of the slice.

<a id="implementation-mc_knlcurrenttime-answers-64-bits-of-epoch-milliseconds"></a>

#### `MC_knlCurrentTime` answers 64 bits of epoch milliseconds

The specification declares it `M_Int64 MC_knlCurrentTime()`, counting
milliseconds since 1970. Getting either the width or the epoch wrong fails in a
way that does not look like a clock problem.

**The width.** A 64-bit result comes back in `r0:r1`, low word first. Answering
in `r0` alone leaves a stale `r1`, so a title that subtracts two 64-bit reads
gets a difference built from a stale register. One title's loading screen draws,
reads the clock, and spins until the difference passes a deadline; with the high
word unwritten the deadline is never reached. The symptom was the instruction
ceiling twenty seconds later, reported against the timer callback the loop ran
in, with the loop's drawing calls filling the platform-call trace and nothing
pointing at time. Raising the ceiling twenty-fold bought twenty times the
frames. **A slot answering 32 bits may write `r1` freely**, since `r0` through
`r3` are scratch across a call, so the answer is always written as a pair.

**The epoch.** The difference between two reads is right whatever the origin,
which lets a wrong epoch survive: only a title that formats, stores, or takes
the time modulo something shows it. The origin is a fixed date rather than the
host's, for the same reason the guest clock is virtual: a run must replay
identically for a frame diff to mean anything.

This is the only 64-bit return among the implemented slots; the kernel,
filesystem and graphics blocks are `M_Int32` throughout.

<a id="implementation-0x97-answers-the-programs-application-id-and-a-titles-copy-check-reads-it"></a>

#### `0x97` answers the program's application id, and a title's copy check reads it

`0x97` takes the numeric program id `MC_knlGetCurProgramID` answers and returns
a **pointer to that program's application id as a string**. Neither the
specification nor the original runtime names it; its callers settle it. One
title does this during startup:

```c
memset(buffer, 0, length);
id  = MC_knlGetCurProgramID();
str = slot_0x97(id);
MC_knlSprintk(buffer, "%s", str);   /* uppercase in place afterwards */
...
if (strcmp("0002E829", buffer) != 0)
        goto notice_screen;          /* draws MODEL / CTN / 인증, then exits */
```

`0002E829` is the AID the archive declares in its own `app_info`. This is the
anti-piracy check: a copy that reached a handset other than through the store
answers something else, and the title stops at an illegal-download notice. The
answer is therefore the archive's declared AID, read from the archive rather
than chosen here, which is what a handset that downloaded it would say.

An archive with no AID, or a program id this platform is not hosting, gets a
null, which the caller already handles by testing the pointer before
formatting it.

This was the only thing stopping one title, which now boots. A second title
recorded as blocked by the same wall was never blocked: its connection-failure
dialog needed its confirm key.

<a id="implementation-block-nine-is-the-utility-block-and-two-calls-named-it"></a>

#### Block nine is the utility block, and two calls named it

Blocks past the filesystem do not follow the specification's section order, so
a block is identified by what its functions do. One title's "authenticating"
screen identified this one with two calls in three instructions: it passes
`0x385` a port and keeps a halfword, and passes `0x388` a pointer to
`"218.50.3.88"` and keeps a word, storing both side by side in one record. A
host-to-network short and a dotted-quad address are the second and fifth of the
specification's six utility functions in its own order, so the block is
`MC_utilHtonl`, `MC_utilHtons`, `MC_utilNtohl`, `MC_utilNtohs`,
`MC_utilInetAddrInt`, `MC_utilInetAddrStr` at `0x384` through `0x389`. All six
are implemented, not only the two reached.

The guest is little-endian, so both byte-order directions are the same swap.
`MC_utilInetAddrInt` answers as a little-endian `inet_addr` does (first octet
in the low byte, so the word's bytes in memory are network order) and answers
`-1`, the specification's documented failure, for anything that is not a dotted
quad. Nothing here resolves names.

Refusing these two as network calls would have been a hard-to-see error: they
never touch a network. The refusal belongs on the socket call the title makes
with what they returned.

<a id="implementation-block-eight-is-the-component-block-and-0x320-is-not-a-clock"></a>

#### Block eight is the component block, and `0x320` is not a clock

The same rule names block eight and corrects a long-standing reading: `0x320`
had been taken for the unidentified block's time call and answered with epoch
milliseconds. It is `MC_uicCreateApplicationContext`.

One title's character-naming screen shows it in three consecutive calls:
`0x320` with no arguments, keeping the result; `0x321` with a pointer to
**`"TextComponent"`**; and a third slot with both, in that order.
`MC_uicCreateApplicationContext`, `MC_uicGetClass` and `MC_uicCreate` are the
first three functions of the specification's component section, and
`MC_UIC_TEXT_COMPONENT` is defined as exactly that string. The arithmetic agrees
(every identified block starts at a hundred times its number, and eight hundred
is `0x320`), as does the neighbour: block nine is the utility block at nine
hundred. Nothing rested on the old reading; only this title reaches `0x320`.

**A component cannot be created here, and that is the answer rather than a
gap.** These widgets are drawn by the handset over the game's screen and typed
into with the handset's editor; answering a handle and drawing nothing would
leave a title waiting for text from something invisible, which is worse than a
readable refusal. `MC_uicGetClass` and `MC_uicCreate` answer `M_E_ERROR`, the
context call answers a token, and the other thirty-eight slots of the block are
accepted no-ops, the same shape and reason as the other WIPI platform's
component table.

The title takes the refusal and draws its own naming screen: a bordered field
holding the default name, with `OK:완료` and `CLR:삭제` under it, on the
platform's text-entry path.

<a id="implementation-network-authentication-is-a-path-the-titles-already-handle"></a>

#### Network authentication is a path the titles already handle

Several titles authenticate over the network at startup. The network block
refuses every call, and that refusal is **not** a wall: the authenticating
titles run a state machine that reads its result as a one-character server code
(`'1'`, `'2'`) or a negative transport error, and every negative path ends at
"인증에 실패하였습니다. 다음 실행시 다시 인증을 시도합니다" — a notice the
player dismisses. Behind it the game boots, saves and plays.

The project also holds patched copies of three of those titles, each differing
from its original in two bytes inside `binary.mod` (`cmp r0, #0` →
`movs r0, #2` for one publisher, `ldr r3, [r2]` → a branch past the block for
another). **The patched copies are not needed**: all three originals reach the
same in-game state, because the refusal was already handled. Nothing here has
to recognise or defeat an authentication routine.

<a id="implementation-mc_netsocket-is-at-0x7d0-which-is-not-where-the-blocks-order-puts-it"></a>

#### `MC_netSocket` is at `0x7d0`, which is not where the block's order puts it

Two titles reach `0x7d0` from inside the `MC_netConnect` callback, and it is
`MC_netSocket(domain, type)`. The caller passes the specification's
`MC_AF_INET` (2) and `MC_SOCKET_STREAM` (1), tests for a non-negative
descriptor, and on success calls `MC_netSocketConnect` with the address and
port prepared by the `MC_utilInetAddrInt` and `MC_utilHtons` calls just above.

**The number is the finding.** Counting from `MC_netConnect` at `0x258` puts
`MC_netSocket` at `0x25a`, and it is not there. The rest of the block did not
move: granted a descriptor, the same module resolves `MC_netSocketConnect` at
`0x25b` and `MC_netSocketClose` at `0x25e`, where the count puts them. One entry
of the network block sits two thousand slots from its neighbours, `0x25a` is
left unclaimed rather than filled by a count this entry disproves, and the
string.h lesson (a run of slots between two known ones is not free to be
anything else) has a counterexample on this platform.

It answers `M_E_ERROR`, as the rest of the block does: there is no network, and
a title refused a socket takes its existing offline path. Both titles do. One
shows "서버와 연결이 끊어졌습니다 / 다시 연결하시겠습니까?" and waits; granting
the socket and letting `MC_netSocketConnect` fail reaches the identical screen,
which shows the refusal arrives where the title is listening.

<a id="implementation-mc_fslist-is-0x19a-and-a-listing-is-more-than-the-archive"></a>

#### `MC_fsList` is `0x19a`, and a listing is more than the archive

`0x19a` was the one file-block entry with a place in the order and no
implementation. A title reaching it settled the contract: it zeroes a 1024-byte
stack buffer, calls `0x19a(dirName, buffer, 1023, accessLevel)`, treats zero as
success, and walks the buffer as NUL-terminated names until an empty one,
reading each as a three-digit number because it is listing a folder of song
folders.

The answer is the directory's immediate children, one path segment each,
packed NUL-terminated and ended by an empty name, with `M_E_SHORTBUF` for a
buffer that cannot hold them. A file two levels down contributes its parent's
name once.

**The names needed a second index.** A `SaveStore` answers about keys it is
handed and cannot be walked, so a listing built from the archive alone shows the
files a title shipped and none it wrote; a title that lists a directory to find
its save slots would always see it empty. `fs/.created` is therefore kept like
`fs/.removed`, for the same reason: what the store cannot represent is recorded
beside it. A path the archive packages is not recorded, because the listing
finds it anyway.

<a id="implementation-strtok-is-one-past-where-the-list-puts-it-and-free-two-past-malloc"></a>

#### `strtok` is one past where the list puts it, and `free` two past `malloc`

The C table's string.h run was placed by counting the specification's list from
`strcpy`, which left one slot unaccounted for between `strlen` at `0x411` and
`memcpy` at `0x414` and predicted `strtok` at `0x412`. A title now calls it at
**`0x413`**. The call site is unambiguous: the title passes a text table and a
one-character delimiter, hands the answer to `atoi`, then calls **the same
function pointer** with a null first argument and the same delimiter.
Continuing from saved state on a null argument is `strtok` and nothing else in
the list, so `0x412` remains unknown.

`strtok` had been left out because it is the one function in the list carrying
state between calls, where a wrong contract corrupts a parse silently. The
state is the scan position, kept on the client because that is where a
handset's single static lives. The title's trace checks it: over one route the
title names a buffer twice and continues sixty-nine times, alternating a comma
inside a line with a newline at its end, each answer one segment further.

`free` is at **`0x428`**, two slots above `malloc`, again established by a
caller rather than a count. The title reaches it through the C idiom
`if (p) free(p)`, and every guarded pointer is a live block from `malloc`'s
heap; over a whole session none is an address this platform did not issue.
`0x427` is unknown.

**Both were found from the failure message, not a disassembler.** An
unimplemented C or WIPI C slot now reports what the Java side already did: the
calling address, the four argument registers, and what each word is (an object
this platform issued, printable text, or the first words it points at).
"argument 0 is the text \"GGI\", argument 1 is the text \"XPlayerURL\"" showed
`0x428` takes a pointer and made two caller instructions worth reading; the
disassembly then confirmed it.

<a id="implementation-slots-that-are-accepted-without-being-understood"></a>

#### Slots that are accepted without being understood

A slot that a real module reaches, that has no known contract, and that is
answered anyway goes in `internal/platform/lgt/wipic_unknown.go` with its
reason, because these fail differently from an unimplemented slot: an
unimplemented one stops the game and says so, while one of these lets the game
believe it was served. Only a slot reached by a real module qualifies, and a
test fails when one is added without a reason or when a recorded slot turns out
to be implemented.

The list was meant to shrink, and **it is now empty.** Three entries left once
the specification named them, a fourth when its argument shape did, and the
last when its caller was disassembled. Both media-block slots that got that far
sit past `MC_mdaVibrator`, where the ordering is the vendor's own and matches
no edition of the specification's list, so neither could be read off its
number.

**`0x4c2` is `MC_mdaSetWaterMark`.** One title calls it with `(clip, 100)`
between creating and playing a clip and ignores the result. Exactly one media
function takes a clip and a 0-100 value that is not a volume: the watermark, the
point at which a streaming clip raises `END_OF_DATA` or `FULL_OF_DATA`. It
returns void, and its default is 100%, so the call asks for the value it
already has. The specification carries it in the HAL appendix rather than the
ordered `MC_mda*` list, which is why the ordering never reached it. Nothing here
streams, so the slot is implemented as a clip-handle check and nothing else
(`wipic_media.go`).

**`0x4ce` is the default volume of a system volume category, asked for by
name**: the specification's `MC_mdaGetDefaultVolume`, whose argument is a
numbered category id where this vendor took the name.

Reading the caller rather than the call showed it. The trace says
`0x4ce(name, 2, 0xffffff)` with the result unused, and for several sessions that
was all the evidence: three arguments whose last two constants settled nothing,
and an ignored return. The code at every call site (the same vendor SDK routine
compiled byte for byte into each module across three title families) says
otherwise:

```
movs r0,#0 / bl backLight   ; leaves 2 in r1, and r2 holds what it held
ldr  r0,[pc,#..]            ; the only argument: "GENERAL"
bl   0x4ce
adds r1,r0,#0               ; the answer, straight into
bl   <clamp to 0..100, store as this object's volume>
```

One argument, and the result is read. **The `2` and the `0xffffff` are
registers `backLight` left behind**, and the clamp to 0..100 is the WIPI volume
scale. `"GENERAL"` is the specification's first system volume category (the
set a handset reports through the `"DEFAULTVOLUME"` property: `GENERAL, VOICE,
RING, KEY, MESSAGE, ALARM, ALERT, MMEDIA, GAME, OEM`), the one an ordinary
application plays under. Every local caller asks for it.

The slot answers the system volume, which `MC_mdaGetVolume` reports and
`MC_mdaSetVolume` moves, so a title that reads the level and restores it on exit
round-trips. As an unknown slot it answered zero, telling six titles they had
started silent. **Nothing observable changes in the local archives**: their
first nine hundred ticks make an identical sequence of platform calls either
way, because this platform does not attenuate by volume and no local title
branches on the stored level. The correction still matters, because a stored
zero can be handed back through `MC_mdaSetVolume` and become this platform's
volume.

**A trace reports registers, not arguments.** Where a slot's argument shape
settles its contract, the trace points to the call site and the call site is
the evidence.

<a id="implementation-character-input"></a>

#### Character input

The five slots at `0x12c`–`0x130` are the specification's input-method calls,
in its own order:

| Slot | Call |
|---|---|
| `0x12c` | `MC_imGetSurpportModeCount` |
| `0x12d` | `MC_imGetSupportedModes` |
| `0x12e` | `MC_imSetCurrentMode` |
| `0x12f` | `MC_imGetCurrentMode` |
| `0x130` | `MC_imHandleInput` |

They were read as a record database for several sessions, because the
specification's third section is the database. The callers settle it: one
engine's text-entry widget makes four of the five calls in its constructor:

```c
count = 0x12c();                                  /* stored in the widget   */
modes[0..3] = 0, 1, 2, 3;
p = 0x12d();
if (strstr(p[0], "/L"))      { modes[0] = 0; modes[1] = 1; }
else if (strstr(p[0], "/S")) { modes[0] = 1; modes[1] = 0; }
widget->mode = 2; 0x12e(2);
```

`0x12c` is called with no arguments set up (the registers still hold the
widget's own), as a call to a `void` function looks, and its result is kept as a
count. `0x12d` answers a `char **` the widget dereferences and hands to
`strstr`. `"/S"` and `"/L"` come from the specification, which says a supported
mode is an ISO 639 language code, a script with case appends `"/S"` or `"/L"`,
and digits are `"N123"`. The fifth call comes from the widget's key handler with
six arguments, four in registers and two on the stack, `(key, type, buf1,
size1, buf2, size2)`: exactly `MC_imHandleInput`'s signature, and the "caller
that looks like a record read" that kept the database reading alive.

The four simple calls are answered as the specification defines them: `EN/L`,
`EN/S`, `KO`, `N123`, a mode index kept and reported back, and `1`/`0` rather
than an error code from the setter. A widget ends in the same state as before
(the reading it takes from `"EN/L"` produces the mode table its constructor
started with), so adopting the real contract changed no title's frames.

**The browser owns letter and Hangul composition.** In `N123` mode
`MC_imHandleInput` returns one completed ASCII digit for a pressed or repeated
numeric key. For external text the Host sends one character-key event while
holding a completed, strictly EUC-KR string, and the platform substitutes that
string in the widget's normal `MC_imHandleInput` call. This uses the phone or PC
QWERTY keyboard, not an emulated manufacturer keypad; the widget keeps its text,
cursor, deletion, field limit and mode selection.

Earlier probes saw only constructor flushes. A later authentication acceptance
route reaches a mandatory name widget that selects mode 3 and sends `2` and `3`
with event type 502; empty answers prevented name entry. The corrected path enters
digits and accepts a four-digit name, while the guest's own minimum-length check
rejects a two-byte name. This supersedes the earlier claim that no local title
sends character keys through the platform. The
[WIPI input-method contract](https://mirusu400.github.io/wipi-wiki/hal/input-method.md)
defines numeric mode and the completed/composing output buffers. Numeric entry
also remains available through the game keypad.

**The composing buffer is now written.** `(key, type, buf1, size1)` arrive in
registers and `(buf2, size2)` on the stack, and a wrongly recovered stacked
argument writes into the game's stack rather than failing, so it waited for a
confirming caller. The widget builds two eight-byte buffers on its stack, stores
their addresses at `[sp]` and `[sp+4]`, and the platform stub balances its push
before the supervisor call, so the handler sees the caller's stack pointer. The
specification marks both sizes as input capacities, but the widget reads those
words back as output byte lengths, so the platform reads both capacities first and
writes the completed and composing lengths afterwards; leaving `16` in either made
the widget consume stale stack bytes and corrupt adjacent state.

`type` uses the Clet boundary's vendor event numbers (pressed 502, released 503,
repeated 504); the observed widget forwards 502. A flush empties both buffers
regardless of event type.

Because a Clet writes the framebuffer directly, a surface is **re-read from
guest memory before every draw call and written back after**, and
`MC_grpFlushLcd` re-reads before presenting; see [A Java title's drawing does not
synchronise, and a Clet's has to](#implementation-a-java-titles-drawing-does-not-synchronise-and-a-clets-has-to).

<a id="implementation-the-two-arc-slots-come-from-their-neighbours"></a>

### The two arc slots come from their neighbours

`0xd8` and `0xd9` were blank, and a blank slot in this block is not a stub:
`handleWIPICSVC` ends the session on one, so a title reaching for a curve
stopped there.

The anchor rule in "The WIPI C slots" placed them without disassembly. `0xd7` is
`MC_grpCopyArea` and `0xda` is `MC_grpDrawString`, both confirmed, and the
specification's function order puts exactly two entries between them:
`MC_grpDrawArc` then `MC_grpFillArc`. The same argument fixes the other ARM
platform's 15 and 16, whose table has the same two neighbours at 14 and 17.

The signature is the rectangle calls' with two more integers in the middle:

```c
void MC_grpDrawArc(MC_GrpFrameBuffer dst, M_Int32 x, M_Int32 y,
                   M_Int32 w, M_Int32 h, M_Int32 s, M_Int32 e,
                   MC_GrpContext *pgc)
```

Eight arguments, so **the context is the eighth, not the sixth**; reading it
where the rectangle calls keep theirs would take an angle for a context
pointer. `s` is a start angle and `e` an *extent*, zero degrees is three
o'clock, positive angles run counter-clockwise, and the arc is centred in the
rectangle `x, y, w, h`. A non-positive width or height draws nothing, as the
specification states.

The Java side had the matching hole and a worse one: `Graphics.drawArc` and
`fillArc` were unimplemented, and `fillRoundRect` and `drawRoundRect` were
wired to the plain rectangle, so every rounded panel came out square. All four
now walk `internal/curve` and fill through the same clipped, translated fill as
the rectangle calls. See `ktf.md`, "The same geometry, on five surfaces", for
why that geometry is in one place.

<a id="implementation-drawing-the-context-belongs-to-the-game"></a>

### Drawing: the context belongs to the game

Every `MC_grp*` call that writes pixels has the same shape —

```c
void MC_grpFillRect(MC_GrpFrameBuffer dst, M_Int32 x, M_Int32 y,
                    M_Int32 w, M_Int32 h, MC_GrpContext *pgc)
```

— the destination surface first, **the context last**, and the colour is a
context field rather than an argument. `MC_GrpContext` is a structure the game
allocates; the platform never holds one. `MC_grpInitContext(pgc)` fills it with
defaults, and `MC_grpSetContext(pgc, index, pv)` changes one named field.

This runtime first modelled it the other way round, reading the first argument
as a handle to its own context object and the last as a colour. Nothing
matched, so every fill, line and string was refused, while titles that draw
their own sprites into the framebuffer looked as if they worked. The cost was
every background: a notice screen filled white with red text came out red on
black.

The callers confirm the field identifiers: one title sets index 1 six hundred
times while drawing (a 16-bit pixel), index 0 with a pointer when it changes
what it may touch, and index 7 once at startup: foreground, clip and font. The
structure keeps the clip as four `uint16` (left, top, right, bottom), and a clip
that does not describe a rectangle is taken as the whole surface, because a game
that never set one would otherwise draw nothing.

<a id="implementation-the-clip-is-an-array-of-four-numbers-and-the-far-corner-is-outside-it"></a>

#### The clip is an array of four numbers, and the far corner is outside it

The data behind that pointer is **not** the structure's own packing. The
specification's row for `MC_GRP_CONTEXT_CLIP_IDX` says:

> 클리핑 영역. `pv`에는 정수 배열이 들어가며 … 배열의 첫 번째·두 번째 원소에는
> 클리핑 사각형의 좌측 상단 x, y 좌표가, 세 번째·네 번째 원소에는 우측 하단 x, y
> 좌표가 들어간다. 좌측 상단 점은 클리핑 영역에 포함되며, **우측 하단 점은
> 포함되지 않는다.**

Four `M_Int32`, with the bottom-right corner one past the rectangle. The row for
`MC_GRP_CONTEXT_OFFSET_IDX` gives the offset the same shape with two.

This platform copied words straight across in both directions, two faults with
one cause:

- **Setting** a clip took the caller's `x1` for a whole corner and its `y1` for
  the other. A clip of (0,0)-(240,320) arrived as left 0, top 0, right 0, bottom
  0, failed the rectangle test and became the whole surface, so full-screen
  clips seemed to work and **no partial clip ever narrowed anything**.
- **Reading** one back filled two of the four elements. One title asks for its
  clip to decide what part of a map to redraw, computes a width and height from
  elements the platform never wrote, and draws nothing: its maps and
  backgrounds came out black while its sprites, which ignore the clip, drew
  normally.

The transfer is now element by element (four for the clip, two for the offset)
between the game's `M_Int32` array and the structure's halfwords, and a draw's
clip is `right-left` wide rather than `right-left+1`, because the far corner is
outside. `MC_grpInitContext` writes the far corner as the width and height
rather than one less, for the same reason.

**Effect over all 94 local archives**, 900 ticks each, every frame compared
between builds: **91 byte-identical and three different, all three drawing
more.** One had painted a stray black band across the top of its notice screen;
one drew an empty dialogue frame whose text never arrived; one gained the
border and soft-key label of a dialogue it already drew. No title lost a pixel
and no summary gained a `tick_error`. The reporting title, driven past its
notice screen with a key, went from a white screen and then a black one with a
line of text to its publisher logo and then its map, sky, trees and character
(141 of 161 frames).

<a id="implementation-one-build-draws-twenty-four-rows-below-everything-the-platform-draws"></a>

#### One build draws twenty-four rows below everything the platform draws

The first title the clip fix made draw put its picture twenty-four rows too low,
leaving the rows below as whatever the display held before. Its text was placed
correctly and its picture was not: a disagreement about where row zero is.

It is not this platform's drawing. Refusing the clip entirely, or turning every
`armcore` blit recogniser off, leaves the frame byte-identical. The title
composes its frame in an off-screen buffer with its own code and puts it up
with one `MC_grpCopyFrameBuffer(0, 0, 240, 320)` per frame; the offset is
already in that buffer.

**The guest's own arithmetic shows it.** Its blit computes

```
destination = base + 2 * ((x - x0) + stride * (y - y0 + extra))
```

where `extra` is a word in its graphics state, read only when an adjacent byte
says the target is the display rather than a sprite buffer. Initialisation sets
it once, immediately after `MC_grpGetDisplayInfo`,
`MC_grpGetScreenFrameBuffer` and `MC_grpInitContext`:

```
adds r2, #0x90
ldr  r3, [r2]
adds r3, #0x18      ; twenty-four, a constant
str  r3, [r2]
```

Twenty-four is not computed from anything this platform answers: the constant
is the same with any `PHONEMODEL`, and `-screen` does not move it. Writing zero
into that word once mid-run makes the whole screen correct (sky at the top, the
name tag under its character, the status bar at the bottom), so only the origin
is wrong.

**The build expects the raw screen framebuffer pointer twenty-four rows above
the visible top of the LCD**, as a handset with an annunciator strip above the
client area would hand it over. This platform hands the visible origin.

**Emulating the strip is not a platform-wide answer.** The same franchise's
other Clet writes its first pixel into row 0 and renders correctly, so the two
builds assume opposite conventions; a twenty-four-row band above every screen
surface would fix one and break the other. Nothing in either archive's metadata
separates them (both declare an empty `Resolution`), the title calls no
unnamed slot, and a per-title patch would encode exactly the guess
`binary_hooks.go` refuses on the other platform: matching by shape rather than
by game. It was left as a known difference at the time. The explicitly
requested, exact-module exception added on 2026-09-15 now removes this build's
addition during initialization; see [the reproduction and correction boundary](../platform-compatibility.md#lgt-lgt-visible-framebuffer-origin-2026-09-15).

<a id="implementation-mc_grpgetrgbfrompixel-answers-through-its-pointers"></a>

#### `MC_grpGetRGBFromPixel` answers through its pointers

```c
M_Int32 MC_grpGetRGBFromPixel(M_Int32 pixel, M_Int32 *r, M_Int32 *g, M_Int32 *b)
```

The three channels go back **through the pointers**, and the return value is the
pixel it was handed, unchanged. This platform had packed the channels into the
return value (the shape of its counterpart `MC_grpGetPixelFromRGB(r, g, b)`)
and written nothing, so the caller's `r`, `g` and `b` kept stale values and a
title that takes a colour apart to rebuild it drew with three unset numbers.

<a id="implementation-a-java-titles-drawing-does-not-synchronise-and-a-clets-has-to"></a>

#### A Java title's drawing does not synchronise, and a Clet's has to

A surface exists twice: as the runtime's `[]uint16` and as bytes in guest memory
at its allocated address. `syncFromGuest` and `syncToGuest` keep them in step,
and **which is needed depends on who writes pixels**:

- **A Clet is handed the framebuffer's address** (the purpose of
  `MC_grpGetScreenFrameBuffer`) and writes pixels directly, bypassing every
  `MC_grp` call, so its draws must read the surface back before blending and
  write it out after. Nothing here can be dropped: instrumented over 1,200
  ticks, two Clets had changed pixels in 600 and 601 of the 1,201 syncs, two
  million pixels between them.
- **A Java title never gets an address.** It draws only through `Graphics`
  objects the platform hands it, so the runtime's copy is the only one written.

Syncing on the Java path anyway was long **the single most expensive thing this
platform did**. Every drawing call read the whole surface out of guest memory,
converted it a byte at a time into `uint16`, drew, and converted and wrote it
all back: 150 KiB each way for a call that might set one pixel, tens of
thousands of calls a scene. A host profile put `syncToGuest` and
`syncFromGuest` at **55% of the emulator's time**, against 4.2% for the whole
ARM interpreter.

The same instrumentation settled the Java case: **1,711,725 round trips in one
title's session and 82,017 in another's, none of which found a pixel the guest
had changed.** They were removed, and the invariant is restored once a frame
instead: the flush publishes the finished frame to guest memory rather than
reading it back, because on this path the runtime's copy is newer. This assumes
nothing on the Clet side touches a Java title's surfaces between frames, which
holds because a Java title registers no Clet and calls no `MC_grp` slot.

Measured over **all 28 local archives**, 900 ticks each, host nanoseconds per
guest instruction before and after:

| | before | after | |
|---|---|---|---|
| the four Java titles | 705.5 / 614.7 / 207.8 / 207.4 | **67.5 / 170.3 / 38.9 / 39.2** | 3.6x to 10.5x |
| the twenty-four Clets | 6.47 – 222.2 | the same | none slower by as much as 2% |

and on the two runs driven to where a title is working:

| | before | after |
|---|---|---|
| one Java title, 1,500 ticks | 701.5 ns a step | **67.3** |
| the other, a whole in-game session | 366.6 ns a step | **41.0** |

That session went from 150.9s of host time to 16.9s (25.5ms a tick to 2.9ms,
1.96 times the guest clock to 17.5) while retiring the same 411,577,534 guest
instructions. Every frame of both Java runs (750 and 2,484) was byte-identical,
so the cost bought nothing any behaviour relied on.

<a id="implementation-the-copy-the-clet-path-keeps-was-the-largest-thing-left-on-it"></a>

##### The copy the Clet path keeps was the largest thing left on it

The round trip stays on the Clet path, where it cannot be dropped, and there it
remained the largest single cost outside the interpreter. A host profile of one
title's field scene, reached by a route:

| | share of host time |
|---|---|
| `armcore.Engine.Run`, everything under it | 68.9% |
| `syncToGuest` | 8.7% |
| `syncFromGuest` | 7.9% |
| `runtime.kevent` and `runtime.madvise` — the collector | 9.5% |

Counted over the same run: **29,631 reads back and 28,727 writes out, 153,600
bytes each**. The collector row is the same fact: both functions allocated a
scratch buffer per call, so a twenty-second scene allocated **8.66 GB, 95% of it
those two buffers**, on the machine also running the guest.

Neither the buffer nor the loop was needed. The runtime's pixels are
`[]uint16`, guest memory is little-endian halfwords, and every cross-compile
target is little-endian, so the two are the same bytes and the transfer is the
memmove `Memory.Read` and `Memory.Write` already do. The byte-at-a-time loop
rebuilt each of 76,800 pixels from two bytes it had just copied.
`armcore.ReadHalfwords` and `WriteHalfwords` are that transfer without the loop
and the buffer (`docs/armcore.md`, "Bulk halfword transfers").

| one title's field scene, same route and save, release build | before | after |
|---|---|---|
| host time for the run | 20.08s | **15.62s** (−22%) |
| ns per guest instruction | 7.34 | **5.71** |
| allocated over the run | 8.66 GB | 448 MB |

All 886 frames the route paints are byte-identical and the run retires the same
2,735,911,562 guest instructions, so the surfaces did not change. Afterwards the
pair is **1.2% of the run** and `handleDraw` as a whole 1.8%.

**The 14% is why it is a cheaper copy rather than no copy.** Instrumented over
that scene, only 4,173 of the 29,631 read-backs found a guest-changed pixel, but
they found 19.6 million pixels between them; the 86% that find nothing cannot
be told from the 14% without reading the surface or having the core report
which pages a guest wrote. The latter is worth building only if this pair climbs
back up a profile; at 1.2% it is not.

<a id="implementation-the-field-numbers-are-the-specifications-and-index-3-is-a-gap"></a>

#### The field numbers are the specification's, and index 3 is a gap

The standard defines the identifiers one constant at a time, with the number in
the prose: `CLIP` 0, `FG_PIXEL` 1, `BG_PIXEL` 2, `ALPHA` **4**, `PIXELOP` 5,
`PIXEL_PARAM1` 6, `FONT` 7, `STYLE` 8, `XOR_MODE` 9, `OFFSET` 10. Index **3 is
unassigned**; a reader counting the names in order lands one short from `ALPHA`
onwards and wrongly concludes the platform's numbering is skewed.

`PIXEL_PARAM1` and `XOR_MODE` are also missing from `MC_grpSetContext`'s own
table of indices, though the `ALPHA` row names `PIXEL_PARAM1`. Treating that
table as the authority on how many fields exist produces the off-by-one.

<a id="implementation-a-pixel-operation-is-a-function-the-game-installs-not-a-colour"></a>

#### A pixel operation is a function the game installs, not a colour

Index 5 holds a **function pointer** and index 6 its parameter. A title that
sets one tells the platform how each drawn pixel combines with the pixel
already there:

```c
M_Int32 op(M_Int32 srcpxl, M_Int32 orgpxl, M_Int32 param1)
```

One local title sets index 5 eighty-five thousand times in a run, with values
like `0x2a75` and `0x23bd`. Read as 16-bit colours, they sent an earlier
investigation after a nonexistent field-numbering error. **They are Thumb
addresses**: the low bit is the Thumb bit, and the module's code lives in
exactly that range. Disassembly settles it without a run:

- `0x23bc` is eleven bytes: `if (arg1 == key) return arg0; return arg1;`, with
  `key` a colour loaded from a global. Plain colour-key transparency.
- `0x2a74` does the same key test, then adds the two pixels channel-wise in
  RGB565 (`>>11 & 0x1f`, `>>5 & 0x3f`, `& 0x1f`, clamped at 31, 63 and 31,
  shifted back and recombined). An additive blend, i.e. an effect.
- `0x2618`, which a run later showed is used most, is the same key test followed
  by `(incoming - existing) * w + (existing << 8) >> 8` per channel, with `w`
  from a second global: a linear interpolation, an alpha blend the title
  implements itself.

**A title can do its own alpha, so the unread `ALPHA` index was not the gap.**
This one never sets index 4; it installs the blend as a function.
`MC_GRP_CONTEXT_ALPHA_IDX` is still stored and not read, and per the
specification setting it implicitly sets `PIXELOP` and `PIXEL_PARAM1`, so a
title using it instead of installing a function remains a separate case; no
local title has been seen to.

**Each of these takes its key and weight from a global, not `param1`.** A run
logs `param=0` throughout, consistent with the disassembly.

**The arguments are not in the order the names suggest, and this platform is
not KTF.** The specification's prose is explicit where its names are not:
`srcpxl` is "프레임 버퍼에 있는 픽셀 값", the pixel *already in the
framebuffer*, and the second is the one about to be written. Its alpha example
proves it: `(srcpxl * (255 - param1) + orgpxl * param1) / 255`, with 255 fully
opaque, must answer the incoming pixel at 255. Both local operations agree: each
compares **argument one** against its transparent-colour key, which only makes
sense if argument one is the pixel being drawn; keying on what is on screen
would hide sprites at random.

KTF's titles pass them the other way round (see "the specification's prose for
those parameters is the wrong way round" in
`internal/platform/ktf/wipic_pixelop.go`, where two operations return their
*first* argument in the ordinary case). Each reading is right for its platform
and neither should be ported. `TestDrawRunsTheGuestOperationWithTheFramebufferPixelFirst`
pins this one with two operations differing by one instruction: `bx lr` answers
argument zero and leaves the surface untouched; `adds r0, r1, #0; bx lr` answers
argument one and paints it.

Every LGT draw path funnels through one `put`, so honouring the operation is a
one-place change. It runs on the thread already servicing the call, because the
guest is inside a draw with its stack pointer where it left it, and answers are
cached by pixel pair, because the operation is a pure function and a draw would
otherwise call guest code per pixel. The specification makes drawing modes
exclusive (setting one cancels the other), so XOR wins over an operation rather
than compounding with it.

**The cache is per operation.** This title alternates between its colour key and
its alpha blend about twice a tick, so a single cache keyed by "the current
operation" was discarded 3,879 times over 2,000 ticks and gave back most of its
savings. The cost is invisible in a frame (output is identical); it was found by
counting the platform's operation-installed log lines. The caches are held
together, bounded at eight operations and 32,768 pixel pairs each; exceeding
either bound costs speed, not correctness.

A word this platform did not write is not trusted to be a function: only a
Thumb address inside the loaded module is taken, because a title that leaves a
handle or colour in that field would otherwise have the platform branch to
non-code. The rule comes from a KTF title that stores a font handle in the
equivalent word.

<a id="implementation-what-the-change-actually-did-to-a-screen-measured"></a>

#### What the change actually did to a screen, measured

The same tree was built twice, once with the operation path forced off, and the
two runs diffed over one route. 1,938 of 1,998 frames differ, all inside the
bounding box of the consent panel on screen throughout, and the whole
difference is **one colour pair over 17,361 pixels**:

```
RGB565 (4, 11, 5)  ->  (4, 10, 4)      red unchanged, green -1, blue -1
```

Findings:

- **The operation touches exactly one fill.** The panel's white text, green
  button and black pixels are bit-identical between builds; nothing else on this
  screen goes through the alpha operation.
- **The delta is at most one step of a 565 channel**, the smallest difference
  the display can express, which is why almost every frame differs while the
  builds are indistinguishable by eye.
- **The result is a fixed point of the blend.** `((in - ex) * w + (ex << 8)) >> 8`
  returns `ex` unchanged whenever `in - ex` is 0 or 1, so a channel that settles
  one step below the drawn colour stays there. A panel redrawn every frame
  through a linear interpolation *could* have drifted toward the background and
  gone black over a few hundred frames; the same delta at tick 100 and tick 1999
  rules that out.

**The measurement does not give the weight.** An earlier reading took the
difference to prove a weight near 255, assuming a black pixel behind the panel;
solving the pair for both unknowns, *every* weight from 0 to 256 admits some
background producing it. Nothing depends on `w`. Likewise green and blue moved
one step while red did not, because red's existing and incoming values were
already equal, so the guarantee is "no more than one step per channel".

The visible result here is a quantisation step; the dramatic screens use the
**additive** blend at `0x2a74`, which no route has reached.

**A title that installs no operation cannot be affected**, as the code shows: the
branch is taken only for a validated Thumb pointer in the context, and every
other draw follows its previous path instruction for instruction. Three titles
run to confirm it reached only opening notice screens, before any installation; a
later pass drove one title through **15,490 ticks of route to real gameplay** on
both builds, no operation installed, **0 of 7,267 frames differing** against a
zero noise floor.

A title installing an operation *during* gameplay remains unobserved. **All 28
local LGT archives booted for 600 ticks log exactly one install**, this section's
title on its opening screen; the boot window would miss an operation installed
three screens in, but no second title installs one on the way in.

**The reported screen has not been reached.** Nobody has driven this title to the
battle effects of the original report, which the additive blend serves; this
records the mechanism, the disassembly and one measured screen, not a claim that
the effects now look as the handset drew them.

<a id="implementation-the-text-face-is-the-handsets-small-one-on-both-paths"></a>

#### The text face is the handset's small one, on both paths

Text, both a Clet's `MC_grpDrawString` and a Java title's `Graphics.drawString`,
is drawn and measured with `glyph.Handset()`, and the three font slots answer
that face's metrics rather than fixed numbers. It used to be the 16-dot face,
which the 240x320 screen every LGT archive declares suggests.

**The screen does not predict the font; the boxes show it.** A game lays out
from the metrics it was written against, then draws into that layout. At 16
dots a notice screen sized for eleven syllables a line holds ten, and the
eleventh is clipped at the right edge: the last character of four consecutive
lines of one title's usage notice was cut in half. At the handset metrics the
same strings fit with room to spare. This is the finding the KTF runtime's
`fontFace` records, reached independently here, and it is one face for every
screen size for the same reason.

Only three of the twenty-four local archives draw any text through this
platform; the rest carry their own bitmap font and layout and are
byte-identical across the change. A frame diff across the whole library named
the three in one pass, so a screen outside them cannot be explained by the
font.

<a id="implementation-magenta-is-the-mask-colour-and-a-surface-copy-leaves-it-behind"></a>

#### Magenta is the mask colour, and a surface copy leaves it behind

`0xf81f`, pure magenta in RGB565, is transparent to `MC_grpCopyFrameBuffer` and
`MC_grpDrawImage`. It is a property of the platform, not of a call: neither
argument list carries a colour key, and the titles here never set the context's
transparency field.

What a caller must be able to do settles it. One engine renders all its text
this way:

```c
MC_grpFillRect(strip, 0, 0, w, 10, pgc);     /* foreground = 0xf81f */
p = MC_grpGetFrameBufferPointer(strip);      /* draws the glyph itself */
MC_grpCopyFrameBuffer(screen, x, y, 12, 12, strip, sx, sy, pgc);
```

It has no other blit, and its outlined-text path is the same copy run four more
times at one-pixel offsets. Copying opaquely put a magenta box behind every
character on every screen of that title, which looked like a palette fault.

The mask is the last resort. A surface carrying its own transparency keeps it:
a decoded image whose encoding declared transparent pixels (a PNG's alpha, or
the palette entry a BMP names in its header; see `wipic.DecodeBitmap`) is drawn
by that declaration alone, because a picture allowed to say what it means may
paint in the mask colour. A handset bitmap decoded through `wipic.DecodeLBMP`
declares nothing (the format has no transparency this build reads), so it gets
the mask; see `ktf.md`, "LBMP: the handset's own bitmap", for the format and why
every platform's image router now reaches it. The mask applies only where
nothing was declared, which covers the surfaces a title builds for itself.

The risk of a fixed key is a genuinely magenta pixel becoming a hole; the
convention exists because art for these handsets avoids the key colour. This was
measured: turning the mask on left the frames of six other local titles **byte
for byte identical**, so nothing here paints with it by accident. The title it
was found on ships neither PNG nor BMP; it decodes its own formats and writes
pixels through the framebuffer pointer, so no header could have declared
anything.

<a id="implementation-timers"></a>

### Timers

`MC_knlDefTimer(MCTimer *tm, TIMERCB cb)` registers a callback **against the
game's own timer structure**, and `MC_knlSetTimer(MCTimer *tm, M_Int64 timeout,
void *parm)` arms it. The callback is `void (*)(MCTimer *tm, void *parm)`.

Getting either of two details of the arming call wrong runs the game's data as
code:

- The 64-bit timeout occupies `r1` and `r2` **without** the even-register
  alignment a modern ABI would give it, so the parameter lands in `r3`. Reading
  `r3` as the callback, as this runtime did, branches to the game's parameter;
  it faulted inside the game's data at an address that named nothing.
- The callback belongs to the structure, not to a handle: a title defines a
  timer once and arms it a thousand times, passing only `tm`.

<a id="implementation-input"></a>

### Input

A key reaches a Clet through its `handleCletEvent`, and **the event kind is the
WIPI Java key event type plus 501**: 502 pressed, 503 released, 504 repeated.
Both engines here agree: one subtracts `0x1f6` and `0x1f7` from the kind and
branches on zero, the other compares against `0xfb << 1`. The key code is the
WIPI value a card's `keyNotify` would receive, the same set KTF uses, so a Host
key mapping serves both platforms.

Sending 1 and 2 delivers events every title ignores, which looks exactly like
unwired input.

<a id="implementation-nothing-repeats-a-held-key-and-no-title-has-asked-it-to"></a>

#### Nothing repeats a held key, and no title has asked it to

The specification has a third kind, `MV_KEY_REPEAT_EVENT` ("fired every so
often while the key stays down"), and a `KEYREPEAT` system property answering
`"600:250"`-style timings: the first repeat after 600ms, then one every 250ms
until release. This platform generates neither: a key is a press and a release,
and `KEYREPEAT` is not among the properties.

This was checked because a browser keypad is held with a thumb, and a game that
acts only on presses would move one step per tap, which would look like jerky
movement. Two measurements rule it out:

- **No local archive contains the string `KEYREPEAT`**, across all 64 KTF, LGT
  and SKT archives and their JARs. Nothing asks for the cadence.
- **A held key already moves a character across a map.** Holding a direction on
  the title reported as stuttering walks the character out of the starting area
  into the next in 120 ticks. The titles track press and release themselves.

Repeats therefore stay unimplemented: there is no caller, and a cadence invented
for none would become a contract to keep.

<a id="implementation-a-host-repeat-is-not-a-second-press-and-forwarding-it-dashed"></a>

##### A Host repeat is not a second press, and forwarding it dashed

**The Host's key vocabulary has three actions, not two.** A browser learns of a
held keyboard key through the operating system repeating the keydown (roughly
every hundredth of a second after a half-second delay, at a user-configured
cadence this project does not control), and `web/app.js` forwards those as
`repeat`. The session layer translated one into a press, because a Clet has no
other kind.

That invents an event no handset delivers: **a second press with no release
between**. Two local titles read a press as a fresh tap and dash on it, so a held
direction dashed again on every OS repeat, reported as "it dashes over and over
like auto-fire". Replaying an in-game route twice, identical up to the first
repeat, the runs differ over more than half the screen four ticks later, and the
repeated run draws the character mid-dash with the after-image trail the title
draws only for a dash.

A repeat therefore delivers **nothing** here (`lgtKeyEvent` in
`internal/session`). A held key is a press and a release, as the handset sent
and as these titles track; the same route with OS repeats injected now
reproduces the plain run's screen at every checkpoint. The earlier KTF package
has the same shape (a pressed and a released application event, no third) and
drops a repeat for the same reason (`ktf.md` "The title takes keys as events").
The two platforms with their own repeat event keep receiving one (WIPI Java's
`keyNotify` takes the type, a MIDP `Canvas` has `keyRepeated`), but **no longer
the Host's**: the OS cadence is wrong, as measured below, so the Host drops it
and generates the specification's cadence on its own clock (`session.md`, "A
held key repeats on the handset's clock").

**The cadence was the fault, not the repeat.** A press repeated at the
specification's timing (first after 600ms, then every 250ms) leaves the dashing
title drawing frames identical to no repeats. Only the OS cadence, thirty a
second, reads as a run of taps. This bounds what a repeat may cost if one is ever
needed, explains why the fix below sends a press at a state change rather than on
a clock, and is the measurement the handset cadence rests on for the two
platforms that take a repeat.

<a id="implementation-a-release-stops-a-character-the-pad-has-moved-on-from"></a>

#### A release stops a character the pad has moved on from

Dropping the repeats uncovered a fault they had hidden, reported as "it walks and
then just stops". The player's log of the moment:

```
press LEFT
press DOWN      0.3s later, left still down
release LEFT    31ms after that
release DOWN    3.3s later — and the character stood still for all of it
```

A title here keeps **one direction and drops it when a pad key goes up**. That
suits the control these games were written for (a thumb rolls from one direction
to the next, and a pad cannot have two down at once) and not the one they are
played with here, where fingers hold two arrow keys and release them in any
order.

Two measurements isolate the release:

- **Both directions held, nothing released: the character keeps walking.** A
  second direction arriving does not stop it.
- **The attack key stops it with no release.** Pressing a direction, then the
  attack key without letting go, stops the character just the same. That is the
  title's own doing, which is why only the pad is treated specially below.

`Session.SendKey` therefore gives the title the pad rather than the keyboard: a
pad key released while another is down delivers **nothing**, because the pad
still reports a direction. Only two things reach the game: a press for the
direction now under the thumb, when the released key was the one in use, and a
release when the last pad key comes up, naming the direction the title was
given. The rule is in [`internal/keypad`](../../internal/keypad/keypad.go),
because the MIDP runtime needed it too; a platform supplies only which codes are
the pad.

**The pad is eight keys, not four.** Besides the four navigation codes, 2, 4, 6
and 8 are the same pad under the digits, as these titles were played on a keypad
and as half the local routes drive them. The other digits are not the pad: one
title puts skill shortcuts on 1, 3, 7, 9 and 0, the corners the pad does not
use, and an extra press there would be an extra cast.

**The rule that failed** looks obvious: re-announce the held direction with a
press after *any* key goes up. It fixes the stop and makes a title dash at
random. Rolling around the pad presses the next direction and lifts the previous
one forty milliseconds later, so the re-announcement lands a fraction of a second
after that direction's real press: a double tap, which is a dash. The player
found it within a minute ("I never dashed once and it dashes while I turn"). The
shipped version adds no press the player did not make.

Two more measurements bound it:

- The specification's repeat kind, 504, delivered at 600/250 while a key was
  held, produces **byte-identical frames** on both reported titles: neither
  reads it, so the tidy answer is inert.
- A run with one key held is **byte-identical before and after** on both
  titles: nothing changes until two pad keys overlap.

This assumes a title keeps one direction rather than a set, which the reported
stop proves for the two measured titles (a title holding a set would have
cleared one bit and walked on). A title that walks diagonally by holding two pad
keys would instead keep the released one until the pad empties; check that first
if a character drifts sideways.

<a id="implementation-a-facing-is-read-off-the-sprite-and-it-needs-magnifying-to-be-read-at-all"></a>

#### A facing is read off the sprite, and it needs magnifying to be read at all

One title was reported turning the wrong way: pressing left and attacking left
turns the character right. No trace answers that, because a facing is the
title's own state and reaches the platform only as sprite blits, so it must be
read off the screen, where **a character on a 240-wide screen is about twenty
pixels tall.** At 1:1 a contact sheet cannot tell the facings apart; at five
times, with pixels repeated rather than smoothed, the eye and the sword are
unmistakable. That is what `wfeature zoom` is for ([`cli.md`](../cli.md)); the
missing piece was the tool, not a run.

With it, the facing is **correct in isolation**: a short press of left, right or
up turns the character that way, and the following attack and the frames after
it keep that facing. The report concerns an attack with an enemy on screen, so
what remains is reaching one; the facing itself is no longer unknown.

<a id="implementation-the-guest-clock-advances-with-work-not-only-with-ticks"></a>

### The guest clock advances with work, not only with ticks

`Session.Tick` moves the clock by one tick, which is not enough alone: a title's
loading screen spins on `MC_knlCurrentTime` waiting for a deadline **inside one
Clet call**, where the Host cannot insert a tick. The clock never moved, the
wait never ended, and the run died on the instruction ceiling with the same
timestamp on every read.

The clock therefore also advances with instructions retired, at the rate of the
handset this platform stands in for (`guestInstructionsPerMillisecond`). Work is
measured from the last tick, not from the start, so the two do not compound: a
tick sets the floor and moves the baseline, and instructions carry the clock
from there. The rate sets what a spin-wait costs (a 100ms wait spends fifteen
million instructions) and, because a frame's computation is charged to the
guest clock, whether a title can reach its requested frame period. The second
job chose the number; see "What the rate has to be for a title to get the period
it asks for", below.

**The floor is the larger of the two, not their sum.** A tick is how much time
the Host says has passed; work is how much of it the guest was busy. A frame
spending 43ms of guest work inside a 50ms tick took 50ms, 7ms idle. Adding them
made it 93, running every title at about **1.9x speed**: every animation, timer,
cutscene and spawn rate uniformly wrong, with nothing visible in a screenshot.
Work wins only when it overruns the tick, the case the work clock exists for: a
loading screen that burns three seconds of instructions in one call moves the
clock three seconds.

Measured numbers: a title in the world retires about 2.16M instructions per tick
and a world load 165M in one, which at the current rate are 14ms of work against
a 50ms tick, and 1,100ms. (Both were read when the rate was 50,000, giving 43ms
and 3,300ms; the instruction counts are the measurement, the milliseconds the
rate applied to them.)

<a id="implementation-the-host-has-to-pace-the-ticks-because-nothing-else-does"></a>

#### The Host has to pace the ticks, because nothing else does

The clock is virtual end to end: it moves with ticks and retired instructions,
never because time passed. The Host's tick rate is therefore the game's speed,
and a Host waiting a fixed span between ticks sets that speed to
`tick / (its own tick cost + that span)`, a ratio of two numbers unrelated to
the game.

It fails in both directions, looking like two unrelated bugs. Through cheap
menus (10ms a tick against a 16ms fixed wait) a title ran at **1.79-2.48x**.
In-game on a slower machine (57.7ms a tick) the same title ran at **0.68x**:
slow motion at a steady frame rate, because nothing dropped frames; the guest
was told less time had passed than had. The steady frame rate hides it: it looks
like the game's own speed, not a Host falling behind.

`Session.TickFor` is the paced entry point a Host on a real clock must use: a
tick is due every session tick of wall time, and it reports the remaining wait.
That is zero once the tick overruns, since a tick costing more than it
represents cannot be bought back, and the debt is capped at one tick so a world
load inside one call is not repaid by sprinting through the next scene. Measured
at 1.00x steady, 20 ticks a second. `Session.Tick` still steps unpaced, which a
batching Host (`runlgt`, the acceptance probes) wants: the same sequence,
faster.

<a id="implementation-a-tick-that-stands-for-a-fixed-span-rounds-every-frame-rate-up"></a>

#### A tick that stands for a fixed span rounds every frame rate up

Pacing fixed the ratio but left the granularity, which was most of the reported
"everything on this platform is slow".

**A title's frame loop here is a timer that re-arms itself at the end of every
frame**, so the requested interval *is* its frame rate. What the local titles ask
for, read off `MC_knlSetTimer`:

| title | asks for | got, on a 50ms tick | now |
|---|---|---|---|
| two of one series | 46ms | 101.7ms | 56.9ms |
| a fighting title | 1ms | 50.0ms | 19.0ms |
| a side-scroller | 83ms | 100.0ms | 83.1ms |
| a dungeon title | 60ms | 102.2ms | 80.1ms |
| a platformer | — | 101.0ms | 72.1ms |
| a licensed title | 20ms | 50.1ms | 27.3ms |

A tick that always stands for 50ms fires timers only on a 50ms grid, so **every
interval is rounded up to a multiple of the tick**, and worse, because the guest
arms the timer partway through a tick. One title asks for 46ms, arms it about
9ms into a tick, lands 5ms past the next one and waits a further whole tick:
100ms of guest time for a frame it wanted at 55. Half speed, at a steady frame
rate, with no number showing it: the same shape as the ratio bug.

The fix: **a tick stands for the wait until the guest's next scheduled work**,
bounded above by the session tick, and `TickFor` waits for the span the tick
stood for. The ceiling keeps a Host taking frames and delivering keys while a
game waits out a long timer; nothing else changes, and a title with no armed
timers ticks as before.

**A floor belongs on the period, not the tick.** A title asking for a one
millisecond timer means "as soon as you can", not a thousand frames a second; on
a handset it got its own frame time rounded up to the timer's resolution. The
specification says a timer is accurate to "the timer resolution the operating
system underneath supports", and error is expected. The guest's work is the
first bound (the work clock measures it and `advance` takes the larger of the
two), but that is nearly zero for a title idling behind such a heartbeat, so the
period is also floored at one frame of a sixty hertz display. That number is not
a claim about any handset; it is the fastest a player's screen can present.
**Flooring the tick instead was tried and is wrong**: it rounds every interval up
to a multiple of the floor, the original mistake in miniature (the 46ms title
came out at 64.5ms rather than 56.9).

Measured across the local set with `-ticks 600`, before against after, as guest
milliseconds per frame: 101.7→56.9, 50.0→19.0, 50.1→47.6, 100.0→83.1,
102.2→80.1, 101.0→72.1, 102.3→68.3, 230.0→200.0, 50.1→27.3. Nothing is slower.

<a id="implementation-the-wall-has-to-be-charged-for-the-work-not-for-the-intent"></a>

#### The wall has to be charged for the work, not for the intent

The section above says `TickFor` waits for the span the tick stood for; it did
not. `tickOnce` returned `tickSpan()`, the span it *set out* to stand for, while
the clock moved by `max(work, span)`, so **every millisecond a title spent
computing was handed back free**. In a scene whose frames cost more than their
interval the guest clock outran the wall by **2.27x**: a local title painted a
frame every 44ms of real time, each carrying 100ms of its own.

This third failure of the family looks like neither of the others. It is not
slow motion or a frame rate: frames arrive often and evenly, each moving the
world further than the interval it was shown for, so motion steps rather than
travels.

**Both existing pacing tests passed throughout**, because the fixture module
returns from every slot at once, so a fixture tick has no work to overrun its
span. The two tests that pin it now supply the work through the clock's step
source (the one thing a fixture cannot make expensive), and both fail on the old
code with the numbers above: `TestATickReportsTheGuestTimeItActuallyStoodFor`
and `TestTickForPacesGuestTimeWhenTheWorkOverrunsTheSpan`.

`advance` reports what it applied and `tickOnce` returns that. Paced on the same
route and save, the guest went from 2.27x the wall to **0.95x**.

**Neither other platform can have this defect**, because neither has a work
clock. KTF's guest clock is the wall clock (a `ManualClock` that jumps to each
next deadline only when a batching Host runs it, `ktf/clock.go`), and SKT's is a
pace clock over the wall (`skt/runtime.go`). Computation costs their guests
nothing, so their guest time cannot outrun the wall. Their failure is the
opposite: a Host that cannot finish a frame before the next deadline runs late,
a throughput problem rather than a clock one.

<a id="implementation-what-the-rate-has-to-be-for-a-title-to-get-the-period-it-asks-for"></a>

#### What the rate has to be for a title to get the period it asks for

A frame's computation is converted to guest milliseconds at
`guestInstructionsPerMillisecond`, so **that rate decides whether a title can
reach its own frame period**: the same rounding as above, with the handset's
speed in place of the tick's granularity.

The measured title asks for 55ms. At 50,000 instructions a millisecond it spent
100ms of its own clock on the average frame, and a third of its frames cost
twice the request. Paced, same route and save, wall milliseconds between
paints:

| rate | p10 | p50 | p90 | p99 | frames a second | busy |
|---|---|---|---|---|---|---|
| 50,000 | 48 | 81 | 146 | 215 | 9.7 | 25.3s |
| 100,000 | 42 | 55 | 84 | 120 | 14.2 | 20.9s |
| **150,000** | **52** | **55** | **60** | 105 | 15.7 | 22.0s |

At 150,000 the wall interval between frames is the requested interval, within
5ms at p90, and matches the guest's own: wall p50 55ms and p90 60ms against
guest p50 55ms and p90 56ms.

**7% remains**: the guest runs at 0.93x the wall, not throughput-bound (21.8s of
host work inside 57.3s), from tick cost and sleep overshoot the capped debt
cannot repay. A seventh of the previous error, in the direction that does not
break animation, it is recorded rather than chased.

**Raising the rate cannot make a title run fast.** A tick advances the clock by
`max(work, span)`, `span` being the wait until the guest's next scheduled work, so
the clock never steps past a due timer; a higher rate only shrinks `work`. The
knob saturates at the title's own schedule.

The cost is throughput: the same computation stands for less guest time, leaving
the Host proportionally less real time; the stand-ins in `armcore.md` paid for
that. Swept over the 30 local archives (boot, 1,500 ticks each, guest time
against host time):

| | at 50,000 | at 150,000 |
|---|---|---|
| titles held back by the rate | 6 | 0 |
| worst period asked-for vs got | 193ms | 77ms |
| lowest headroom in the set | 1.05x | **1.02x** |
| next lowest | 2.02x | 1.29x |

The six held-back titles gain most (193→77ms, 142→71ms (p90 221→85), 68→50,
64→41, 38→24, 27→20); the rest are unchanged because they already met their
period on the tick alone.

**The title with no headroom does not pay**: 1.05x before, 1.02x after, because
its boot loop is bounded by its own timer, not its computation, so its host cost
does not change. The rate decides only how much of a title's clock its
computation eats.

**Confirmed from the player's side.** Two browser-logged sessions of the same
title in the same field scene: the earlier build reported 11.9 to 15.4 frames a
second across eleven windows; this one 17.9 to 18.4 across eleven on a desktop
and 18.0 to 18.4 on a phone. **18.2 is 1000/55**, the requested period. The
spread falling from 3.5 to 0.5 is what the player described, more than the
third-higher frame rate. Guest speed reads 0.99 to 1.02x, nothing was dropped,
and host cost fell (26-33ms a tick to 10-14ms; 40% of a core on the desktop, 47%
on the phone), because the old build computed a fast-running world it could not
show.

**A load now reads slow and is not.** Both sessions show one 0.34x window crossing
into the field: a world load inside a single Clet call. The instructions and wall
time are unchanged; only the clock's charge moved, from 3,300ms to 1,100ms, and
the stats windows stalled for 3.21s before against 2.88s after.

The number is not a claim about a handset (nothing local or in the specification
names one) but the point where the corpus stops being held back.
`TestRateSweep` is that measurement; `docs/testing.md` has how to rerun it. Every
local archive's first frame is byte-identical before and after except one, which
reaches **further** in the same 64 ticks (its "press any key" line drawn), and the
acceptance runs for all three platforms pass unchanged.

<a id="implementation-a-java-titles-frame-loop-is-not-a-timer-and-it-lost-a-tick-a-frame-twice"></a>

#### A Java title's frame loop is not a timer, and it lost a tick a frame twice

Everything above concerns `MC_knlSetTimer`, a **Clet's** frame loop. A Java
title arms no timer: its loop is a thread ending each frame with
`Thread.yield()` and `Thread.sleep(n)`. Each cost a whole session tick per
frame, for different reasons, found from a player's report ("it is a little
faster, but it still lags when I move") against a build whose host cost had just
fallen ninefold.

**The tick did not know when the thread wanted to wake.** `tickSpan` asked only
`nextTimerDue`, so a title with no Clet timer always got the full session tick.
It now also asks `nextJavaThreadDue` and takes the earlier, including **zero** for
an arrived deadline; without that zero every wait came out one tick long, because
the servicing tick advances the clock before handing over the slice. A thread
that merely spent its budget is skipped, since its zero means "no deadline", and
treating it as due would stop the guest clock.

**`Thread.yield()` parked.** Parking ends the slice and slices are granted once a
tick, so a yield with nobody waiting cost 50ms of guest time. One title's loop,
`work; repaint; yield; sleep(100)`, was charged **150ms of guest time for the 100
it asked for**. A yield now parks only when another thread could run, bounded by
`javaYieldBurst` at eight free yields a slice, because another title spins on
`yield` while waiting and unparked spent the whole four-million-step budget every
tick (26.7 million instructions became 4.8 **billion**, a run of seconds took
minutes); the cap leaves a frame loop's single yield alone and parks a spin
almost at once.

| the reported title, in game | before | after |
|---|---|---|
| guest ms a frame | 122.7 | **86.4** |
| frames a second | 8.15 | **11.6** |
| what its sleep asks for | 100ms, given 150 | 100ms, given 100 |

Host cost rises as it should, 21.6s for the session instead of the 16.9s measured
above, because the emulator no longer idles through needless ticks and draws 35%
more frames in the same guest time, at 40.4ns a step: twelve times the guest
clock.

**Neither defect shows in host time**, which is why both outlived a profile:
`ns_per_step` and `busy_ms` do not move during idle ticks; only guest
milliseconds per frame does. **In the world, one title's frame goes from 127.3ms
to 87.1ms** over the same scripted route (1.46x); the rest is throughput, not
pacing. `runlgt`'s summary reports `guest_ms` because the tick count no longer
says how much guest time a run covered.

The step budget for one Clet call is sized for the opposite case: entering the
world happens inside one timer callback, where the heaviest local title
decompresses over a thousand resources into new images, and fifty million
instructions stopped it part way, reading as a hang.

**The budget is measured, not chosen**, by bisecting with `runlgt -steps`: that
heaviest load falls between 1.1e9 and 1.2e9 instructions, and the budget is about
two and a half times that. Sizing it by eye had made it 1e9, generous-looking and
one title short of the world. When a run reports the limit, **read the
platform-call trace before raising it**: a callback allocating, reading resources
and copying is working and wants a bigger budget; one repeating the same few
drawing calls is a spin no budget ends. Raising the ceiling twenty-fold on such a
spin bought twenty times the frames and nothing else.

<a id="implementation-files"></a>

### Files

A file is a save entry under the `fs/` scope (shared with KTF's guest filesystem
and SKVM's `XFile`), falling back to the packaged JAR resource of the same name,
so a game reads data it shipped and writes data it did not:

```
var/savedata/<profile>/lgt/<PID>/fs/<path>
```

There is no record database beside it. `0x130` was serviced as one until its
block was identified as the input method's; no title here imports a database
call.

**A directory listing is `MC_fsList` at `0x19a`**, answering from the archive
entries under the directory and from `fs/.created`, the index of paths a title
has written; see "`MC_fsList` is `0x19a`" above for why the index exists.

**A removed file must be recorded as removed.** The save boundary has no delete
and a read falls back to the packaged JAR, so `MC_fsRemove` writes the path into
`fs/.removed`, which a read consults first. Removal used to write an empty file
in the path's place, leaving the path existing, and a title's new-game flow is:

```c
sprintk(path, "Save%d.dat", slot);
MC_fsRemove(path);
... // create the character
if (MC_fsIsExist(path) == 0) { /* a save is here, so this is a continued game */ }
```

A title told its save still exists **skips everything a new game begins with**.
Two titles lost their entire opening sequence and dropped the player into the
world with no prologue or tutorial; nothing looked wrong on screen, so it
survived a run that reached the world and saved.

RMS keeps the same kind of index for the same reason: a `SaveStore` answers only
about keys it is asked for, so anything it cannot represent is recorded beside
it.

The other WIPI platform's guest filesystem had the same shape and was corrected
with it: `FileSystem.unlink` dropped the in-memory entry while the read behind it
still found the persisted save and then the mounted archive. No KTF title here
has been seen to delete and re-ask, so that correction came from this platform's
evidence; see [`ktf.md`](ktf.md#implementation-ktf-platform-implementation-status).

**Read-only is the one open flag that cannot create.** `MC_fsOpen`'s flags are
read (1), write (2, which appends), write-and-truncate (4) and read-write (8);
any but the first has write intent and creates a missing file. Treating a single
bit as "writable" refused the flag a title opens its save with, and the title
retried forever against a handset it read as full. The specification says this
by omission: `M_E_NOENT` is a failure of `MC_fsOpen` **only** for a read-only
open.

**The create happens at the open, not the first write.** A handset has the file
when the open returns, so `MC_fsFileAttribute`, `MC_fsIsExist` and a second open
all answer about a zero-length file from then on. Deferring creation to the
first write left the handle usable and hid the file from all three:

```c
fd = MC_fsOpen("SAVsetup", MC_FILE_OPEN_RDWR, MC_DIR_PRIVATE_ACCESS);
MC_fsFileAttribute("SAVsetup", &info, MC_DIR_PRIVATE_ACCESS);  // M_E_NOENT
```

The title reads that as a filesystem it cannot save to, paints "saving failed,
quitting" over its splash, and calls `MC_knlExit`: before its first screen, every
run, with an empty error log. `M_E_NOENT` is also **not** among
`MC_fsFileAttribute`'s documented failures, so the answer was never expected. One
title died here on every launch; nothing else in the 94-title LGT corpus changed
behaviour when the creation moved, measured at 800 ticks with keys against the
previous binary.

**An empty save tree is not a neutral starting point.** One title's boot reads
its settings file; when the open fails it writes the defaults, clears the byte it
just wrote, paints a screen telling the player to restart the handset, and quits
on the next key. The next launch reads the file and boots into the game. That is
a first-run flow, but every probe starts from a clean save directory, so the
title only ever showed the notice and was carried as broken for several
sessions. **Launch a title that stops at an impassable screen twice against the
same `-save` tree before calling it a defect**: the first run may write what the
second needs.

`MC_fsFileAttribute` fills three words and **the size is the third**, known from
the caller: a title wraps the call twice, returning the word at offset 8 and the
word at offset 4, and reserves twelve bytes. Callers act on the size, allocating
their read buffer with it.

`MC_fsTotalSpace` and `MC_fsAvailable` **take no argument**: they report the
program's storage, not the remainder of an open file. Reading the first register
as a file handle answered an error to a title asking how much room it had, and
it refused to start a game because the handset was full. The reported number is
this platform's own, comfortably larger than any save here and small enough to
stay an ordinary integer.

**`String.valueOf(Object)` answers `Object.toString()`'s form even for a class
that overrides `toString`.** The language defines it as `"null"` for null and
`obj.toString()` otherwise; this platform answers the text
`StringBuffer.append(Object)` appends: the object's own characters when it has
them, `Class@address` otherwise. Honouring an override means a nested guest call
from inside a platform call, and nothing has needed it. **A title that displays
the answer will show `Class@1f0c` where it meant a name**: a wrong label, not a
stopped title, recorded so its origin is known.

`MC_fsMkDir` and `MC_fsRmDir` report success without doing anything, because a
path is a save key and there are no directories.

**`org/kwis/msp/io/File.openOutputStream` hands back a byte sink bound to the
file.** The specification offers the four stream openers as a faster way to move
bytes than `read`/`write` on the File, and a title writes a block and flushes. A
`ByteArrayOutputStream` is an `OutputStream`, so returning one makes every
existing write, flush and close slot work unchanged; the binding to the file is
the whole addition. Bytes drain at the file's cursor on flush, on the stream's
close, and **before the File itself closes**, because a title that writes through
the stream and closes only the File would otherwise lose everything. The sink
empties as it drains, so flush then close does not write a block twice. The
specification's two failures (a file not open, a second stream on one file) are
both `IOException`. `openInputStream` is the read half: it answers the same
stream object a resource read returns, over the rest of the file from its
current position, and the file's position then follows what the stream
consumed, so a title reading a header through the stream and the rest through
`File.read` sees one file.

**Implementing the write half makes the read half reachable.** A title with no
save has never taken its load path, so the first working `openOutputStream`
writes a save the *next* launch reads, and that launch fails earlier, in a
different method, having skipped the new-game path. That is a door opening, not
a regression; the save tree's timestamps and the direction of the failing call
tell them apart.

<a id="implementation-sound"></a>

### Sound

The media block is at `0x4b0`, ordered as the specification's `MC_mda*` list
with one entry missing. Four slots pin the alignment independently:
`0x4b0`/`0x4b1`/`0x4b3` are clip create, free and `putData` at indices 0, 1 and
3, and past index 3 every entry sits one slot below its specification number:
`0x4bd` is stop (14) and `0x4c1` the vibrator (18), both established from title
behaviour. The volume pair between them makes `0x4bf` and `0x4c0` readable as
`MC_mdaGetVolume` and `MC_mdaSetVolume`.

A clip is a guest record so a game can hold its `MC_MdaClip*` and pass it back,
but **the sound's bytes stay on the Host**: the guest never reads them, and a
megabyte of audio in the guest heap is a megabyte the game loses. Playback goes
through `backend.Audio` on the guest clock, advanced once per tick, so a run
batching ticks hears the same sequence as one on the wall clock, faster.

Three details of this block fail silently when wrong:

- **The last four slots take a source, not a device.** `MC_mdaSetMuteState` and
  `MC_mdaGetMuteState` are keyed by source, and the preceding pair is the same
  for volume: one title calls `0x4d0(11)`, stops if it answers -1, hands the
  result to `MC_mdaSetVolume`, then calls `0x4cf(11, level)`. Reading a
  per-source mute as global silences every clip: a title mutes source six during
  startup and then plays.
- **Volume is a percentage.** One title sets a clip to 50, another to 60.
  Nothing here attenuates by it (the synthesiser's velocities carry the mix), so
  the level only has to round-trip, because a game reads the level it found and
  restores it on exit.
- **`0x4ce` answers a named category's default volume**
  (`MC_mdaGetDefaultVolume` with a name for the numbered id); answering zero told
  six titles they had started silent. See [Slots that are accepted without being
  understood](#implementation-slots-that-are-accepted-without-being-understood)
  for the call site.

C clips retain the `MEDIACB` passed to `MC_mdaClipCreate` and deliver
`(clip, status)` between guest frames. STARTED, END_OF_DATA, STOPP, PAUSED and
RESUMED follow the [WIPI media contract](https://mirusu400.github.io/wipi-wiki/c-api/media.md).
Completion follows the mixer's playback state, including when no Host sink is
attached. Repeating clips do not report completion while looping, and playing
an already playing clip does not restart it. Pause/resume still restarts the
score rather than preserving a paused cursor. Java `Player.pause` preserves the
repeat setting for `resume`; `stop` clears paused state, and duplicate play or
resume requests do not create another voice.

Delivery uses a snapshot: callbacks may stop or free their clip without
reentering an unfinished media call. Freed clips discard remaining callbacks,
and each clip's pending queue is bounded to 64 transitions. Repeated stop
does not generate an endless chain of STOPP callbacks.

A local Clet played its startup clip and then requested no further sound
resources, because its stop callback releases the clip. A fresh-save 7,000-tick
title/menu/new-game route produced 68 MIDI messages before this fix and 3,864
after, with five distinct sound resources loaded. This verifies guest scheduling
and recorded MIDI output, not speaker quality or a complete playthrough.

Java `PlayListener` registration is retained but delivery remains unimplemented;
a Java title waiting for that listener can still stall.

`wfeature runlgt <game.zip> -audio out` records what a run played (`out.mid` for
MIDI events, `out.wav` for sampled ones), which is how this was checked without a
speaker.

<a id="implementation-vibration-is-recorded-not-driven"></a>

#### Vibration is recorded, not driven

Both doors onto the handset's motor, `MC_mdaVibrator` at `0x4c1` and Java's
`Vibrator.on(int, int)`, used to answer success without reading their arguments,
on the reasoning that there is no motor. **Whether there is one is the Host's
answer, not this runtime's**: a browser has `navigator.vibrate`, so what was
missing was a boundary to carry the request.

Both now hand the two numbers to the same `backend.Vibrator`, which a Host reads
through `Session.Vibration`. This package does not act on it and does not know
whether there is a motor, whether vibration is turned off, or whether the Host
can vibrate. `internal/backend/vibration.go` holds the contract, with two
non-obvious rules: `level` is a percentage from 0 to 100, not an index, and a
zero duration at a level above zero means *until stopped*.

**Four local archives ask for the Java class, all naming the same method.** Each
resolves three static entries on `org/kwis/msp/media/Vibrator` and names one,
`on(II)V`; the other two carry neither name nor descriptor. `Vibrator.off()` is
served alongside `on` because the neighbouring platform's class declares it and
its guests call it, though no module here names it. That is harmless: the table
is keyed by name, so an unrequested entry is inert, and the descriptor fallback
for unnamed entries cannot reach two entries with no descriptor. Nothing on this
surface is placed by number.

None of the four reaches the call. Each was started on a fresh save directory,
ticked well past its first frame, then driven with every pad key held and
released in turn for several rounds; all presented several hundred frames, none
ended itself, and the request counter stayed at zero. The neighbouring
platforms' titles stopped at the same point, so this is expected: a title buzzes
in play, not on a screen a boot reaches. The path is wired and tested against
its contract, and the first title observed to buzz will be the first evidence
that the arguments arrive as specified.

<a id="implementation-images"></a>

### Images

An `MC_GrpImage` is a framebuffer with a transparency mask beside it.
`MC_grpGetImageFrameBuffer` exists because an image is one, and the title that
reaches this asks for the framebuffer and then its raw pointer, so an image that
was not addressable memory would fail there. Images and surfaces share one
handle space, so `MC_grpGetFrameBuffer*` works on an image without a second
lookup; the LCD therefore carries its own flag, because nothing else stops a
destroy aimed at an image from taking the screen.

- **`MC_grpCreateImage(MC_GrpImage* out, bufID, off, len)` writes the image
  through its first argument** and returns a status. Getting that backwards is
  silent: the caller reads its stale stack slot and carries on with a number
  that was never a handle, as one title did for a hundred instructions before
  asking to allocate a negative number of bytes.
- **Properties four and five are width and height**, as titles on both WIPI
  platforms read them.
- **`MC_grpDrawImage` has exactly `MC_grpCopyFrameBuffer`'s argument list**; the
  mask is the whole difference. RGB565 has no alpha channel, so an image drawn
  without its mask arrives as a rectangle of whatever its transparent pixels
  decoded to.

<a id="implementation-a-surfaces-handle-is-an-address-because-one-title-dereferences-it"></a>

#### A surface's handle is an address, because one title dereferences it

`MC_GrpImage` is a `void *` in the specification, and one title treats it as one.
Right after a create it asks for the image's width and height (which a counter
handle answers fine), then loads the first word the handle points at and stores
into that structure at `+0x24`. With a small-integer handle that reads guest
address `0x4`, and the title died there every run, one screen past its
connection-failure notice.

Every surface therefore gets a record in guest memory and **the record's address
is its handle**. Word zero is what `MC_grpGetImageFrameBuffer` answers, so a
title reading the framebuffer from the structure gets what the accessor would
give; the next three words are the data pointer, bytes per line and dimensions
the specification says a framebuffer holds, in that order. The rest of the
record's 64 bytes is vendor-specific and stays zero. The layout past word zero
is unknown; what matters is that the record is mapped and the title's own, so a
store into an unchosen field lands in the image instead of off the map.

The title stores `0x00ff00ff`, the magenta this platform already treats as the
mask colour. The write is inert here and is independent evidence for that choice
(see "Magenta is the mask colour").

With the handle addressable the title runs through its publisher screens, title
art, character select and a prologue, into the world.

<a id="implementation-mc_grpgetdisplayinfo-takes-the-display-index-first"></a>

#### `MC_grpGetDisplayInfo` takes the display index first

The structure is its **second** argument, as `MC_grpRepaint` leads with the LCD.
Reading the first argument (zero for the primary display) as the pointer wrote
nothing, and the title read its own uninitialised structure. The call does not
fail: one title took four bytes per pixel from stack residue and drew every
screen at twice its width, on a framebuffer whose other half it never touched,
legible enough to look like a palette problem.

`MC_GrpDisplayInfo` is nine words: bpp, depth, width, height, bytes per line,
colour type, and then the red, **blue** and green masks — not the usual order.

<a id="implementation-0xf0-is-mc_grpfillpolygon-and-the-block-has-two-unnamed-slots-in-it"></a>

#### `0xf0` is `MC_grpFillPolygon`, and the block has two unnamed slots in it

A title's tutorial reached `0xf0` and stopped. The graphics block runs from
`MC_grpGetImageProperty` at `0xc8` in the specification's function order, so the
number is a name once the block's two gaps are counted. `MC_grpCopyArea`, the
fifteenth function, sits at `0xd7` rather than `0xd6`; `MC_grpPostEvent`, the
thirty-seventh, at `0xee` rather than `0xed`. Identified slots on either side
fix the first gap (`MC_grpDrawImage` at `0xd5`, `MC_grpDrawString` at `0xda`),
so the offset is +1 after the first gap and +2 after the second. The block's
last two functions, `MC_grpDrawPolygon` and `MC_grpFillPolygon`, land on `0xef`
and `0xf0`.

**Corrected 2026-10-22.** This passage also listed `MC_grpDecodeNextImage` at
`0xeb` among the identified slots. It was not one: `0xeb` was the table's value
from its first commit, with no call behind it. The count was made against the
v1.2.1 list, and the two gaps are the two functions the v2.x order adds to that
list, `MC_grpDrawImageRegion` at `0xd6` and `MC_grpCreateSubImage` at `0xea`;
in the v2.x order, which LGT's blocks follow, this block has no gap at all. So
`0xeb` is `MC_grpDestroyImage`, which 27 local titles call, and
`MC_grpDecodeNextImage` is `0xec`. See
[The image slots follow the v2.x order](#lgt-image-slots-2026-10-22).

A later caller identifies `MC_grpEncodeImage` at `0xed`: six arguments encode a
framebuffer crop into an owned BMP buffer, immediately consumed by
`CreateImage`. The second gap is therefore before that encoder, not between it
and `PostEvent`. [Follow-up evidence](ktf.md#startup-compatibility-2026-09-19-follow-up-entering-a-campaign).

The call settles it without trusting the arithmetic:

```
wipic 0xf0 unnamed(0x30025a58, 0x400fff80, 0x400fff70, 0x4) = failed
```

The first argument is the surface handle whose width the title just asked, the
next two are consecutive stack addresses, the fourth is four, and a fifth follows
on the stack: `MC_grpFillPolygon(dst, xPoints, yPoints, nPoints, pgc)` with a
quadrilateral, the only shape in the block that fits. The colour comes from the
context, set two calls earlier. Both forms are implemented: the outline through
the shared Bresenham, the fill by the even-odd scanline rule of the KTF runtime's
polygon, so a shape covers the same pixels on either platform. The point count is
bounded, because the arrays are the game's memory and a miscomputed count must
cost only a refused call.

<a id="implementation-sprintf-is-a-c-library-slot-of-its-own"></a>

#### `sprintf` is a C library slot of its own

`0x3f7` is `sprintf`, distinct from the kernel's `MC_knlSprintk` at `0x65` and
served by the same renderer. A title formats the path of the resource it is
about to open with it.

<a id="implementation-the-rest-of-stringh-placed-by-its-neighbours"></a>

#### The rest of string.h, placed by its neighbours

An LGT title ran for two minutes and stopped on `unimplemented LGT stdlib slot
0x40b` inside a timer callback. The slot number names it without disassembly,
because the C table is the specification's list in its order and both ends of
the gap were known:

| Slot | Function | How it is known |
|---|---|---|
| `0x405` | `strcpy` | a caller |
| `0x406` | `strncpy` | a caller |
| `0x407` | `strcat` | a caller |
| `0x408` | `strncat` | between `strcat` and `strcmp` |
| `0x409` | `strcmp` | a caller |
| `0x40a` | `strncmp` | a caller |
| `0x40b` | **`strchr`** | between `strncmp` and `strstr` |
| `0x40c` | `strrchr` | ” |
| `0x40d` | `strspn` | ” |
| `0x40e` | `strcspn` | ” |
| `0x40f` | `strpbrk` | ” |
| `0x410` | `strstr` | a caller |
| `0x411` | `strlen` | a caller |

The specification lists string.h as `strcpy, strncpy, strcat, strncat, strcmp,
strncmp, strchr, strrchr, strspn, strcspn, strpbrk, strstr, strlen, strtok`;
counting from `strcpy` at `0x405` lands `strstr` on `0x410` and `strlen` on
`0x411`, both called by titles. **The six in between cannot be anything else**,
so they are implemented together rather than one crash at a time.

The forced run ends there: `memcpy` is `0x414` rather than the predicted `0x413`,
so one unaccounted slot lies past `strlen`. A caller later placed `strtok` at
`0x413`, leaving `0x412` unknown; see [`strtok` is one past where the list puts
it](#implementation-strtok-is-one-past-where-the-list-puts-it-and-free-two-past-malloc).

`strchr` and `strrchr` search the terminator too, so asking for `\0` answers the
end of the string; a caller measuring a string it holds a pointer into depends on
that.

The trace confirmed the name: the title asks `0x40b(buffer, 0xa)` and feeds the
answer **plus one** back as the next call's buffer:

```
stdlib 0x40b strchr(0x400ffeb8, 0xa, …) = 0x400ffed2 from 0x191b1
stdlib 0x40b strchr(0x400ffed3, 0xa, …) = 0x400ffeec from 0x191c1
stdlib 0x40b strchr(0x400ffeed, 0xa, …) = 0x400fff07 from 0x191c1
```

That splits a message on `\n` line by line: `strchr`, not `strrchr`, which would
not advance this way. Replaying the reported session's key presses as a route
reaches the two-line message box the title draws with it, **the screen the run
used to die on**; the run serves 491 calls.

<a id="implementation-the-java-interface-table"></a>

### The Java interface table

Some local titles are not Clets: four archives so far, a count that grew each
time archives were added. Their application code is Java compiled ahead of time
to the same native ARM as the rest of the module, and the class metadata a class
file would carry is handed to the platform through table `0x64`. None of them
runs here. The record below is what they were observed to pass, because neither
the specification nor the original runtime names the table; a running title is
the only source.

They all take the same path, in this order, and stop in the same place:

| Call | Arguments | What it hands over |
|---|---|---|
| `0x64`/`0x03` | 3 pointers | the launcher class's name, `""`, `"true"` |
| `0x1fc`/`0x03` | 3 | the same three |
| `0x1ff`/`0x03` | 3 | the same three |
| `0x201`/`0x03` | 3 | the same three |
| stdlib `0x418` | 3 | `memset(.bss, 0, 4n)` — the string cache |
| `0x64`/`0x07` | 2 | the class list and where its string constants end |
| `0x64`/`0x14` | 11 | the class metadata, below |
| `0x1f8`/`0x17` | 3 | `(0, 0xc8, &out)` — the archive's path |
| `0x64`/`0x82` | 1 | that path |
| `0x1f8`/`0x16` | 4 | `(0, 0x64, 240, 0)` then `(0, 0x65, 320, 0)` |
| stdlib `0x424` | 1 | a **function pointer** into the module |
| `0x64`/`0x06` | 1 | the handle `0x07` answered |
| `0x64`/`0x83` | 4 | the launcher, below |

`0x1fc`, `0x1ff` and `0x201` are tables holding one function each, and
`0x1f8`/`0x17` is the one OEM entry only a Java title resolves. All are carried
on the Java category, since none relates to a Clet's calls.

The four `0x03` calls are one registration repeated on four tables: the module
loads the launcher's name, `""` and `"true"` into `r0`–`r2` once and calls all
four without touching them, which is why the counts above are three rather than
the four a trace shows (the fourth register holds the previous call's value).
`0x1f8`/`0x17` fails loudly in the module (it prints `don't get jar path` through
the stdlib), so the module checks its answer. The two `0x16` calls carry this
platform's screen size as `(0x64, 240)` and `(0x65, 320)`.

A module also carries a small vendor section, `.raptor`: four bytes `RAPT`, a
version, the archive's AID and the feature list
`kernel dlet cldc wipijava lgte midp mmpp`, which names the AOT toolchain that
produced everything below. Every Java archive here was built by the same version,
so the layouts in this section are one toolchain's ABI rather than one title's.

The eleven arguments of `0x14` are six inputs and five outputs. The inputs are
`(classes, fields, staticFields, virtualMethods, methods, staticMethods)`; the
outputs are five `.bss` arrays, **five for six because the class table gets
none.** Output *n* belongs to input *n+1*: the arrays report fields, static
fields, virtual methods, methods and static methods, in that order. The argument
order alone would put the outputs one earlier; three independent facts settle
it, the argument list being the weakest (`internal/platform/lgt/java_metadata.go`).

**Four of the five are `int16`; the last is a table of function pointers.** The
four are indices (a caller loads one with `ldrsh`); the static-method array is
loaded with `ldr` and branched to:

```
ldr  r4, =static_method_out
ldr  ip, [r4, #0xd8]        ; entry 54, a word
mov  lr, pc ; bx ip         ; called
```

For a static or special method **the platform writes the entry point itself**;
for everything else it writes the number the compiled code will index with: the
platform is asked what to run in one case and where something is in the other.

**The six input tables are one contiguous run of memory** in argument order,
which gives each its length: static methods end where methods begin, methods
where virtual methods begin, and so on. In one local title they are 111, 3, 92,
1 and 47 entries. The class table's runs account for only 111, 3, **34**, 1 and
0 of those, and the difference matters:

- The virtual table's first 34 entries are the platform classes the class table
  names; **the remaining 58 are the application's own methods**, whose vtable
  index depends on how large the platform made the class they ultimately extend,
  so the compiler could not assign it.
- All 47 field entries are the application's own, for the same reason: an
  object's field slots follow the platform superclass's.
- The static-method table stops exactly at the platform's 111. The application's
  own constructors are called by address; only a platform one must be asked for.

**A table entry carries only a name and a descriptor, which is enough.** Two
entries can read `j:I` and three classes can declare `a(I)V` without ambiguity.
A virtual entry names one *slot*, and an overriding subclass shares its
superclass's slot, so every class answering to that name and descriptor answers
at the same index. A field entry names one *field* of one class, so a distinct
index per entry is right by construction; the cost is object size, since an
object then has a slot for every field reference in the title rather than its
own class's fields.

The class table's runs say which platform class owns which entries, which the
platform needs to know what to implement, not to number anything.

**All five arrays are now answered** by `linkJavaSurface` in `java_link.go`: the
four index arrays with the identity map, the static-method array with one SVC
stub per entry. A reached stub names its class, method and entry and stops the
title, turning "a Java app is not supported" into "it called
`java/lang/Math.abs(I)I`". Every local Java archive gets through both metadata
calls and stops at the launcher.

The identity map for fields has a measured cost: the four local titles declare
47, 150, 150 and **416** field entries, so an object sized to the largest index
is 1.6 KB in the last, which a title allocating thousands of small objects will
feel. Sizing each class by its own fields needs the class each entry belongs to,
which the table does not say.

The class table is a count followed by 24-byte entries:

```
uint32 count                       /* 30 in one local title */
struct {
        char    *name;             /* "org/kwis/msp/lcdui/Graphics" */
        uint32   unused;           /* zero in every entry seen */
        uint32   staticFields;     /* count << 16 | start */
        uint32   virtualMethods;   /* count << 16 | start */
        uint32   methods;          /* count << 16 | start */
        uint32   staticMethods;    /* count << 16 | start */
} entry[count];
```

Each run is a count in the high half and a start index in the low half; the
starts are cumulative across the classes using that table, which confirms the
reading. The name/descriptor tables are pairs of `char *`:
`org/kwis/msp/lcdui/Card` claims four virtual methods from index 2, and indices 2
through 5 are `serviceRepaints`, `repaint`, `getHeight`, `getWidth`;
`org/kwis/msp/media/Clip` claims one at index 0, `setVolume`. Thirty classes and
twenty-nine virtual methods line up end to end with no gaps.

The classes in that table are the **platform's** API surface (`org/kwis/*`,
`java/lang/*`, `java/io/*`), not the application's. The metadata says what the
app links against and how it addresses it; the app's own classes arrive
elsewhere, most likely through `0x07`.

<a id="implementation-the-slot-that-runs-a-function"></a>

#### The slot that runs a function

stdlib `0x424` is handed one argument, the address of a function inside the
module, and its result is never read. It has the shape of `atexit` but **runs
the function immediately.**

Both readings answer the caller identically, so a title was run each way and its
next requests diffed. Not calling it, the module goes straight to its entry
point; calling it adds `0x64`/`0x06`, whose argument is the handle `0x07`
answered for the class list. The function is a three-instruction thunk into the
module's Java startup, and `0x06` is the only call returning that handle, so
registering the thunk for exit would enter the application's Java without
acknowledging its classes. No name was found (the specification's ANSI-C list
has nothing that runs a callback, and the slot sits past its end).
`stdlib_run_test.go` pins it against a fixture-planted function.

**Running it took the caller's stack.** A guest call made inside a platform call
must run on the *running* thread, whose stack pointer is where the guest left it;
this one ran on the platform's own thread, whose saved stack pointer is at the
top, over the frame of the waiting module function. The launcher builds `argv`
in its frame, calls `0x424`, and hands the platform an `argv` whose first word the
thunk's locals had overwritten, so the class name came back as a fragment that
reads like a class the module lacks. The fix, `callOn` in `client.go`, is one
argument; every platform call entering guest code (a class initialiser, a
lifecycle method, a registered callback) has the same requirement.

**Past `0x424` the title reaches the entry point**, `0x64`/`0x83`:

```
0x83("org/kwis/msp/lcdui/Main", 0, 4, argv)   argv = {"Game", "", "true", "true"}
```

The class it names is **the platform's own**, and the four `argv` words are a
template the module keeps in `.text`: the module asks for the platform's Jlet
launcher rather than to run its own code. What `"Game"` names was not settled at
this point; it is the string the `0x03` calls passed at the start, and no class
the title registers has that name.

<a id="implementation-the-applications-own-classes"></a>

#### The application's own classes

**`0x64`/`0x07` is where they arrive.** Its arguments are a table (a count, a
zero word, that many class handles, sixteen in one local title and nineteen in
another, then the string constants) and an output array for their objects. These
are the application's classes, not `0x14`'s platform surface. `0x07` answers a
handle the module hands back to `0x0c` and `0x06`.

**The metadata block sits `0x4c` bytes before the handle**, and `handle + 8`
points back at it, identifying the pair in every entry of both local titles.
Past the handle the record is a vtable or a member list, as the header says.

**`super` is one of two things**, by where it points: a `char *` name for a
platform class (`"java/lang/Object"`, `"org/kwis/msp/lcdui/Card"`), or another
entry's *handle* for an application class. One title's ninth class points at its
sixth's handle, whose super is the `Card` name.

**Past the handle the record counts its own members** (as first read; [The fourth
round](#implementation-the-fourth-round-where-a-class-record-says-its-members-are)
corrects where the runs start):

```
handle + 0x00   0
handle + 0x04   0
handle + 0x08   the header, = handle - 0x4c
handle + 0x0c   field count n
handle + 0x10   n field records, five words each
                { owner, char *name, char *descriptor, kind, index }
                method count m
                m method records, seven words each
```

The owner word is the handle, so a member record is recognisable alone, and
`index` counts from zero across one class's members. Stepping over `n` fields and
`m` methods lands **exactly** on the next class's header, record after record,
and the names are unmistakably a class file's (`as:I`, `at:Z`,
`paint(Lorg/kwis/msp/lcdui/Graphics;)V`, `keyNotify(II)Z`, `<init>()V`) under a
superclass of `org/kwis/msp/lcdui/Card`; a misread stride produces neither.

**`+0x0c` is the class's vtable**, not "a pointer into a record pool". Where it
points, the compiler built the vtable into the record; where it is zero, the
platform builds one. A class rooted at `java/lang/Object` has a layout the
compiler knows end to end; a class ultimately extending a platform class does
not. Only the latter carries the counted member lists, the platform's layout
input: four of nineteen, six of sixteen, six of sixteen and two of eighteen
classes across the four local Java titles, the substantial ones, since the class
extending `Card` is where the game is written.

An inline vtable reads as:

```
handle + 0x10   the class handle again        /* vtable[-1], in effect */
handle + 0x14   method 0
handle + 0x18   method 1                      /* ... */
```

which is exactly what a call site expects:

```
ldr   r3, [r6]              ; r6 is the object; r3 is its vtable
ldrsh r2, [r5, #2]          ; r5 is an out array; r2 is the slot
add   r3, r3, r2, lsl #2
ldr   ip, [r3, #4]          ; vtable[slot]
mov   lr, pc ; bx ip
```

**An object's first word is its vtable pointer, the word before the vtable is
the class handle, and slot *n* is at `vtable + 4 + 4n`.** An inline vtable's
zeroes are un-overridden `java/lang/Object` slots, filled by the platform;
`+0x24` holds `vtableSize << 16 | inheritedSize`, with `inheritedSize` 11 for a
class rooted at `Object`. **Eleven is `java/lang/Object`'s vtable size in this
ABI**, fixed because the compiler numbered every slot above it.

The rest of the header reads:

```
header + 0x00   accessFlags               /* 0x21, 0x31, 0x601 for an interface */
header + 0x08   char *name
header + 0x0c   void **vtable             /* or zero: the platform builds it */
header + 0x10   super                     /* a name, or another class's handle */
header + 0x14   interfaces                /* or zero: see below */
header + 0x18   uint16 instanceSize       /* in words */
header + 0x1a   uint16 state              /* 2 as loaded; 3 once prepared */
header + 0x24   vtableSize << 16 | inheritedVTableSize
header + 0x2c   void *body[3]             /* compiled ARM, this class's own */
header + 0x44   0xfffffffe                /* sentinel, in every record */
```

<a id="implementation-a-class-with-no-member-list-is-entered-through-its-interface-table"></a>

##### A class with no member list is entered through its interface table

`Thread.start` on a class whose record has a vtable and no members has no member
record to find `run` in. One local title's data branch stopped there ("t declares
no run") with the class, its vtable and its code all in the image.

The answer is at `header + 0x14`, the class's **interface table**:

```
table + 0x00    count
table + 0x04    count pointers, one per entry
entry + 0x00    a char *name, or another record's handle
entry + 0x04    the vtable slot this interface's methods start at
```

The name-or-handle follows the `super` convention: a platform interface arrives
as a name, an application interface as a handle. That title's thread class has
two entries, `java/lang/Runnable` at slot 10 and one of its own interfaces at 0,
and slot 10 of its vtable holds a real code address, so **`run` is a slot rather
than a record**. Its own interface's zero is not a slot: an interface declaring
nothing has no methods to place, and only an interface whose contract fixes the
order can name a method this way. `java/lang/Runnable` declares exactly one
method, which is why this platform resolves it (`readJavaInterfaces`,
`javaInterfaceMethod`).

With it the branch runs: the thread's `run` asks the handset for a property,
compares the answer against a string constant, dials, and shows the title's own
"the connection dropped, reconnect?" dialog, its offline path.

`instanceSize` is the compiler's count and assumes the platform's layout: a class
with ten fields extending `Card` reports 23 words, so the toolchain assumed a
13-word `Card`. Field references go through out arrays, but a platform laying out
`Card` differently and trusting the number allocates the wrong size.

**An object's data is a second block, at `object + 8`, indexed by word:**

```
ldr   r2, [r4, #8]          ; r4 is the object
ldrsh r3, [r5, #4]          ; r5 is the field out array, entry 2
ldr   r0, [r2, r3, lsl #2]  ; the field
```

A class handle is addressed the same way, `[handle + 8]` being its header, so
"the data at `+8`" is one rule for an instance's fields and a class's record.

<a id="implementation-preparing-a-class-and-the-calls-that-do-it"></a>

#### Preparing a class, and the calls that do it

The compiled code reaches a class by its handle (a `.data` address the compiler
baked in) and gates every use on the state halfword:

```
r4 = <class handle>
ldr   r3, [r4, #8]          ; the header
ldrh  r3, [r3, #0x1a]
cmp   r3, #3
blne  0x64/0x0b             ; not prepared: prepare it
ldr   r1, [<the 0x07 handle>]
bl    0x64/0x0c             ; then this, every time
```

**`0x0b` takes a class handle and prepares the class** (the platform lays it out,
builds its vtable and writes `3` into the state), and **`0x0c` takes the class
handle and the handle `0x07` answered** and returns what the code then uses as
the class. Two more read the same way from their call sites, both on failure
paths: **`0x22` is entered with a null receiver** after `cmp rN, #0` and
**`0x23` with an index and a bound** after a failed `blo`: the null and the
array-index throws. They are the two most-referenced slots in a module (five
thousand and three thousand call sites), because every dereference and array
access has one.

**`0x0d` is the class initializer, not the cast failure.** Its call site is the
second half of the pair above and takes two arguments:

```
r0 = <the class 0x0c answered>
ldr   r3, [r0, #8]
ldrh  r3, [r3, #0x10]
cmp   r3, #5
ldrne r1, =<a function in the module>
blne  0x64/0x0d             ; not initialised: run this
```

so `0x0d(class, clinit)` runs the class's static initialiser once and marks the
class initialised. The earlier reading, a type-tag check falling through to a
throw, came from the branch shape alone; the function pointer in `r1` settles
it, and `0x12` below is the check that reading was looking for.

**The two calls test two different state words.** Preparation is tested at
`header + 0x1a` of the module's record and answers 3; initialisation at
`[class + 8] + 0x10` of what `0x0c` returned, and answers 5. That is the
sharpest evidence that **`0x0c` does not answer the module's handle**:
`header + 0x10` of a module record is the superclass word, so answering the
handle would have the module compare a superclass pointer against 5 and then
write 5 over it.

What `0x0c` answers has the shape of an ordinary platform-class object:
`[class + 8]` is the data block every object carries, its word 2 is the class's
name (call sites read `[[class + 8] + 8]` and pass it where a name is expected),
and its word 4 holds the state halfword.

<a id="implementation-the-string-constants-arrive-with-the-classes"></a>

#### The string constants arrive with the classes

The `0x07` table is longer than its class list: a count, a zero word, that many
class handles, then **a column of string constants**, each a `uint16` length
followed by that many UTF-16LE code units. The call's second argument is where
the column ends.

An earlier reading called this "a column of name pointers", reading a Korean
game's constants a byte at a time. The local titles carry 439 to 597 of them:
item names, file paths, `"PHONEMODEL"`.

**The count the second argument declares is not the number of constants**: one
title's array opens with 367 where the list has 489, another with 330 where it
has 439. Its purpose can stay open: a literal-pool search finds its address
referenced only by the `0x07` call, so nothing the title runs reads it.
`readJavaStringPool` in `java_class.go` reads the constants.

**A constant's object goes elsewhere.** The module's Java startup first
`memset`s the first `4n` bytes of `.bss` (439 words in one local title, 0x6dc
bytes), and the helper materialising a constant reads that array:

```
ldr   r3, =<the string pool>        ; the column of constants
ldr   r1, [r3, r0, lsl #2]          ; constant r0
ldr   r0, [<the 0x07 handle>]
ldr   r3, =<.bss>
add   r3, r3, r2, lsl #2            ; &cache[r0]
ldrh  r2, [r1]                      ; the length
add   r1, r1, #2                    ; the UTF-16 units
bl    0x64/0x09
```

**`0x09(group, units, length, slot)` builds one `java/lang/String` and caches
it**; the cleared word makes the helper idempotent. The `memset` length
independently counts the constants and agrees with the pointer arithmetic above,
the only other source.

`readJavaClass` in `internal/platform/lgt/java_class.go` reads the records,
refusing a handle whose back-pointer does not name its header and stopping
rather than walking a count that is not one.

Names are plain ASCII `char *`, not UTF-16 as an earlier reading said; string
constants are UTF-16, because a name is the compiler's and a constant is the
program's.

A failure past the Java table is reported as `ErrJavaAppUnsupported` with the
real stopping point inside it: the stopping slot is a symptom, the cause is that
this is a Java app.

<a id="implementation-the-three-thunks-every-class-carries"></a>

#### The three thunks every class carries

`header + 0x2c` holds three addresses, each a small module function standing for
one thing the compiled code cannot do inline. Together they name every call a
class costs, turning the class record from a description into a protocol:

```
body[2]   "give me this class"        prepare if [header+0x1a] != 3, then 0x0c
body[1]   "give me it, initialised"   body[2], then 0x0d if the state is not 5
body[0]   "declare my members"        looks the class up in the layout table
                                      below and calls 0x13 with what it finds
```

Call sites use `body[1]`. `new` is always three calls:

```
bl    <body[1] of the class>          ; r0 = the class, initialised
bl    0x64/0x0f                       ; r0 = a new instance
bl    <the constructor's own address> ; taken from the method record
```

so **`0x0f(class)` is the allocator**: 172 call sites in one local title, more
than any slot but the two throws, unnoticed because the class is *named* one call
earlier, on `body[1]`.

Nothing in the module calls `body[0]`; its only reference is the class record, so
the platform calls it during preparation, when `0x0b` is handed a class that has
not yet declared its members.

<a id="implementation-the-applications-classes-are-laid-out-by-a-second-class-table"></a>

#### The application's classes are laid out by a second class table

`body[0]` walks a table the module keeps in `.text`: a count, then 24-byte
entries `{ char *name, fields, staticFields, virtualMethods, methods,
staticMethods }`, each run a count in the high half and a start in the low half,
as in the platform class table. It finds the entry whose name pointer matches the
class's and calls:

```
0x13(entry, class, fields, staticFields, virtualMethods, methods,
     staticMethods, fieldsOut, staticFieldsOut, virtualOut, methodsOut,
     staticMethodsOut)
```

which is `0x14` for one application class rather than the platform's thirty:
the same five member tables, the same five output arrays, one entry's runs.

**Only classes whose layout depends on the platform are in it**: nine of twenty
in one local title, three of twenty-three in another, seven of twenty-five in a
third. A class rooted at `java/lang/Object` is absent, because the compiler laid
it out and shipped its vtable in the record. Every class in the table has a
platform class up its chain.

<a id="implementation-the-slot-rules-and-what-they-say-about-the-platforms-own-classes"></a>

#### The slot rules, and what they say about the platform's own classes

An entry's run gives the count; the class record's own header gives the base:

- **A field's slot is `instanceSize - fieldCount` plus its position in the
  run.** `instanceSize` is the compiler's own count at `header + 0x18`.
- **A virtual method's slot is `vtableSize - newMethodCount` plus its
  position**, where `vtableSize` is the high half of `header + 0x24`; but the run
  holds overrides as well as new methods, and an override takes its superclass's
  slot, so subtracting the whole run is right only when the class overrides
  nothing.

The first rule closes exactly in every class of every local Java title: a class
with 52 fields whose superclass reports 25 words reports 77, and its run starts
where the superclass's instance ends. The second closes for most classes and
falls short by exactly the number of overrides for the rest (one class in one
title, five in another), so **an override must be found by name and descriptor
against the superclass's run** rather than counted.

The first subclass of a platform class *defines* that class's size, so only one
check applies to it: the platform class's declared methods must not lie above
where the subclass starts. Later subclasses must agree with the first, and every
later class is checked against its superclass both ways.

The same arithmetic is the only source of the **platform's** layout. A class
extending `org/kwis/msp/lcdui/Card` with 11 fields reporting 24 words makes Card
13 words, and **all three local titles extending it say 13**; Jlet is 6 words in
all three. Vtable sizes vary (Card 25 in two titles and 26 in a third, Jlet 21
and 20), so they are derived per module from the subclasses, not hardcoded.

An application class rooted at `java/lang/Object` needs none of this; its records
report `vtableSize` 11 higher than their own method count, confirming the 11-slot
`Object` vtable noted under [The application's own classes](#implementation-the-applications-own-classes).

<a id="implementation-what-running-one-of-these-still-takes"></a>

#### What running one of these still takes

The interface table is **implemented** as far as the local titles reach it:
`0x0b` prepares a class by calling its `body[0]` and building its vtable, `0x0c`
answers a class object, `0x0d` runs a `<clinit>` once, `0x0f` allocates, `0x09`
builds a string constant *and answers it*, `0x12` is the type check, `0x54`/
`0x55` are the frame markers a method enters and leaves through, `0x1f`-`0x25`
are the exception family, `0xe1`/`0xe2` are `String` and `String[]`, and the four
array calls are below.

What remained is **the platform API surface**: 30 classes and 99 static entries
in one local title, mostly `org/kwis/msp/lcdui` drawing calls already served for
Clets, added to `java_api.go` one measured method at a time (by name where the
module lists the class's virtual methods, by slot otherwise); a missing method
stops the title with its own name.

**A failure names where it came from.** Every stub returns through `lr`, so a
report carries the calling address, and an unknown slot carries the classes of
arguments that are objects this platform issued (both in java.go): the call site
then shows how many arguments were set and what happens to the answer, and the
classes show what the words are.

Nothing should be guessed: a stop naming its own cause beats a title that starts
and misbehaves, and every wrong reading in this section cost a pass to undo.

<a id="implementation-the-two-entries-every-classs-run-opens-with"></a>

#### The two entries every class's run opens with

They answer with **the class**. Each platform class's static run begins with two
entries with no name or descriptor. One's answer goes straight to the allocator,
`0x0f`, and the next call constructs the result; the other is reached just before
a class's first real static call, its answer dropped. A platform class has no
record, so these entries stand in for an application class's two thunks. A first
attempt read them as "initialise this class" and answered zero; the title
allocated an instance of nothing and failed several calls later at the
allocation.

<a id="implementation-two-kinds-of-virtual-call-and-only-one-of-them-is-the-platforms"></a>

#### Two kinds of virtual call, and only one of them is the platform's

A dispatch is `vtable[slot]`, and **where the slot number comes from is not the
same for every class**:

- For a class whose virtual methods the module lists in its class table (the 29
  entries of one local title: `Graphics`'s drawing calls, `Display.pushCard`,
  `Card.repaint`, `Image.getGraphics`, `Jlet.notifyDestroyed` and `File`'s reads
  and writes), the slot is *answered by the platform* through the out array, so
  it maps back to a name and is dispatched by name like a static.
- For a class with none listed (`java/lang/Runtime`, `String`, `StringBuffer`,
  `Thread`, `Random`), **the compiler baked the number**: the call site is a plain
  `ldr ip, [r3, #0x38]` against a platform header this implementation does not
  have, so only the class and slot are known.

The first kind covers every drawing call, which makes a screen reachable without
reconstructing the original platform's vtables. The second kind is reverse
engineering in its own right, starting from the call site: how many arguments it
sets and what it does with the answer.

**Where a call site cannot choose between two receiver-only methods, answering
both ways can.** A boolean-returning and a void slot look identical from the
arguments, but a title branching on the answer takes two paths, so building the
platform twice (answering 0, then 1) and comparing where each run stops shows
whether the answer is read. `Thread` 13 was settled that way in one pass: the two
answers ended in different methods, ruling out the `void` candidates the rule
had ordered behind it.

**A slot is reported under the class a lookup would use** (the class it was
numbered under, not the dispatching class), with the dispatching class named
beside it when they differ. This platform splits some library classes as the
original runtime did (a call on `Clip` is numbered under `BaseClip` and answers
to that name), so naming the dispatching class would send an implementer to
register the method under a name nothing asks for.

A platform class of unmeasured size gets a **larger vtable than it needs**,
because a slot past the end is a branch into whatever follows. An unimplemented
method therefore reports `java/lang/Runtime vtable slot 13 of 64` instead of
executing unmapped memory.

Baked slots identified so far, with evidence (the table is
`javaBakedVirtualSlots`; a slot not in it is reported by class and number, not
guessed):

| Class and slot | What it is | What says so |
|---|---|---|
| `Object` 1 | `getClass()` | receiver only, and the answer is dispatched on |
| `Object` 4 | `toString()` | receiver only, answer used where a String is |
| `Object` 5 | `notify()` | receiver only, dropped, before the monitor is given back |
| `Runtime` 11 | `freeMemory()J` | the numbering below; no call site can tell it from 12 |
| `Runtime` 12 | `totalMemory()J` | the numbering below; **this used to answer `freeMemory`** |
| `Runtime` 13 | `gc()` | 60 sites through a one-line helper, before allocations |
| `Vector` 15 | `size()` | receiver only, and the answer written big-endian into a save buffer |
| `Vector` 19 | `indexOf(Object)` | the numbering, and a site that hands the answer to `new char[n][]` |
| `Vector` 23 | `elementAt(I)` | the numbering, and a site that null-checks the answer and loads a vtable out of it |
| `Vector` 24 | `firstElement()` | receiver only, the answer dispatched on, paired with 27 |
| `Vector` 27 | `removeElementAt(I)` | a literal zero in, answer dropped, right after 24 |
| `Vector` 29 | `addElement(Object)` | one reference in and **the answer dropped**, in a loop counting a local up to a field |
| `StringBuffer` 18 | `append(String)` | one reference in, and the same slot dispatched on the answer |
| `InputStream` 10/11/12 | `read()`, `read([B)`, `read([BII)` | 0, 1 and 3 arguments |
| `InputStream` 14 | `available()` | its answer is the length of the array the caller then reads the whole stream into |
| `InputStream` 15 | `close()` | receiver only, and the caller nulls its reference after |
| `DataInputStream` 25 | `readShort()` | receiver only, stored two bytes wide; the numbering says which of the two shorts |
| `Class` 16 | `getResourceAsStream(String)` | a String in, a stream out, null-checked |
| `Thread` 13 | `isAlive()Z` | the numbering below, and a site that takes the receiver alone and **branches** on the answer |

<a id="implementation-the-numbers-are-not-arbitrary-and-the-rule-is-worth-more-than-any-one-of-them"></a>

##### The numbers are not arbitrary, and the rule is worth more than any one of them

Every row above was read off a call site over several passes. Together they
spell a rule:

> **A class's own methods begin at slot 10, in the order the library class
> declares them, and a method that overrides one keeps the slot the class above
> it gave that method.**

The specification does not state it: its listings are alphabetical, a
*different* order that gives `StringBuffer` and `Vector` numbers other than the
call sites'. The evidence is that thirty-four independently settled slots land
on it exactly, across eleven classes and three inheritance chains, six in one
consecutive run:

- `Object` 1/4/5: `getClass`, `toString`, `notify` at declaration positions 1, 4
  and 5, which puts a subclass's first own method at 10;
- `InputStream` 10/11/12/13/14/15: `read()`, `read([B)`, `read([BII)`, `skip`,
  `available`, `close`, the class's first six in order, without a gap;
- `DataInputStream` 25: its run starts where `InputStream`'s nine end, at 19, and
  `readFully`, `readFully`, `skipBytes`, `readBoolean`, `readByte`,
  `readUnsignedByte`, `readShort` lands on 25. **This settles what a call site
  could not**: the site showed only a 16-bit store, so `readShort` and
  `readUnsignedShort` were indistinguishable; 26 is the unsigned one;
- `String` 10/28/33/34 and `StringBuffer` 10/13/18/22/23: nine more, including
  `String.length` at 10, once read as an oddity ("inside the eleven `Object`
  takes"), is the first own slot;
- `Vector` 15/29: fourteen apart, exactly what the declaration order puts between
  `size` and `addElement`;
- `Thread` 10/13/14: `start`, `run`, `interrupt`, `isAlive`, `setPriority` are
  the first five own methods in declaration order after the three preceding
  statics, and `start` at 10 and `setPriority` at 14 were already known from
  their call sites. **The rule placed 13 between two anchors it did not choose.**

A later pass added twelve more without contradiction, turning the rule from a
summary into a placement tool. Four filled gaps in runs anchored at both ends
(`String` 11, 14, 16, 19, 26 and 29, `StringBuffer` 17 and 27, `Vector` 28,
`DataInputStream` 20, 23 and 28), and three opened runs for classes this platform
lacked (`Calendar` 14 and 19, `ByteArrayOutputStream` 15, 16 and 17, and
`DataOutputStream`'s ten writes from 15). Each was checked against its call site
and agreed: the `indexOf` caller passes a zero where `fromIndex` goes, the
`delete` caller a zero and a length, the `insertElementAt` caller a zero index,
the `readFully` caller an array and the count it just read, the `append(Object)`
caller the exception it just caught, and the `setSeed` caller sixty-four bits in
two registers where every other method on its class takes none.

`ByteArrayOutputStream` shows the rule covering a class through a parent it
never calls: the three writes inherited from `OutputStream` land on 10, 11 and
12, and its own `reset`, `toByteArray` and `size` follow `OutputStream`'s five at
15, 16 and 17. A title writes a record with the first three and collects it with
the second three, and both halves were placed before either was seen.

**A call site plus the rule settles a slot where a call site alone often
cannot.** `Vector` 19 sat unimplemented for a pass because `indexOf`, `contains`
and `removeElement` all take one reference and answer something. The rule
chooses (18 `contains`, 19 `indexOf`, 30 `removeElement`), and the site agrees: it
hands the answer to `new char[n][]` and `new int[n][3]`, a row count, which
`indexOf` answers and a boolean does not. Where the two disagree the site wins
and the disagreement is recorded; none has so far.

The rule also corrected a slot placed by the specification's alphabetical
listing. `Runtime` 12 is `totalMemory`, not `freeMemory`: `gc` at 13 is solid from
its sixty sites, and the declaration order is `exit`, `freeMemory`,
`totalMemory`, `gc` at 10, 11, 12, 13. This platform had answered "what is left
of the arena" at the slot asking "how big is the arena", a different number for a
title sizing a cache off the heap. Both are now answered.

**An inherited slot is reported against the class whose vtable first built the
stub**, not the declaring class: an `Object` slot reached through a Jlet is named
for the Jlet, and `InputStream.close` through a `DataInputStream` is named for
that. A number valid for a class is valid for every class below it, so both
lookups (the baked table and the module's numbering) walk up the chain in
`javaPlatformSupers`. That table is **also the chain the catch test walks**: a
`catch (Exception e)` around a call throwing `IOException` does not match while
every class is rooted directly at Object.

**Slots 1, 4 and 5 are `java/lang/Object`'s**, and the stub a call arrives
through is the one Object's vtable was built with, so one entry answers for every
class that does not override it.

No call site can settle `Runtime` 11 and 12. Nothing reads either: the library
holds one site between them, in a module's startup sequence, and the instruction
after the call loads a literal and branches without touching r0. The numbering is
the whole evidence: `exit`, `freeMemory`, `totalMemory`, `gc` at 10, 11, 12, 13,
anchored on the `gc` confirmed by sixty sites. The same reading placed the WIPI C
kernel block's slots off `MC_knlExit`.

**Both answer a `long`, a second error in the slot.** A method whose descriptor
ends in `J` puts the high word in r1 and the low in r0, as `currentTimeMillis`
does. This one wrote only r0, leaving the caller's stale r1 as the top half, so a
title comparing `freeMemory() < n` would have compared against that. The arena is
megabytes, so the high word is zero, and it is now written.

<a id="implementation-a-type-check-is-asked-with-a-name-or-with-a-class"></a>

#### A type check is asked with a name or with a class

`instanceof` and the catch test share one call, `0x12`, whose second argument
takes **two forms**. Usually it is a name, read from word 2 of a class object's
data, as the array-type call reads one. A title testing an element against an
array type resolves the type first and passes **the class object**: one site takes
`firstElement()` off a vector, loads the element's class from its vtable,
resolves `[C` through `0x0e` and checks the two, the same word in both registers
because the element *is* a `char[]`.

Read as a name, that word is garbage, and the run stopped there. The second
argument is now looked up first as a class this platform issued, and only a word
that is no class is read as a name. Both reach the same chain walk.

<a id="implementation-what-the-running-titles-say-now"></a>

#### What the running titles say now

**All four local Java titles run their own code, and three reach a play screen.**
They open their resources, decode their pictures, start their game thread and
paint their screens. Three go title screen → menu → new game → a map with
characters, an HUD and dialogue, driven from the keypad; the fourth deadlocks on
its own save-backup check when the network is refused, which is the title's code
rather than a gap here (see "A save-backup check a refused connection leaves up
for ever").

The third is the one whose menus stopped at `Vector` slot 19. Its dialogue path
builds a vector of lines to lay out, asks where one sits, allocates `char[n][]`
and `int[n][3]` from the answer, then drains the vector with
`firstElement()`/`removeElementAt(0)`: four slots, all named by the numbering
rule, plus the type check that arrives with a class object. It now walks its
world with dialogue boxes rendering.

**Two were not stuck but waiting for a key.** Both open with a refused
connection, show the refusal in a dialog with a confirm softkey, and wait. A run
without a key script sits there for ever, indistinguishable from a stopped title.
The confirm key takes one through its notice screens into its menu, opening and
world, and the other three screens further, to the check it truly stops on. **A
probe without a key script cannot tell a stopped title from a waiting one**; the
cheapest test is the key sweep in Testing below: the same prefix once per
candidate key, compared on the last frame.

On the way one of them prepares its classes, runs its class initialisers, enters
`startApp`, builds a resource name from string constants and a `StringBuffer`,
opens that resource from its archive, reads it whole, decodes it into a surface,
opens its save file, throws and catches the `IOException` for the missing save,
loads its data through a `DataInputStream`, constructs and starts a `Thread` on
itself, and returns from `startApp` with the game running on that thread. The
platform then paints the pushed card once a frame and hands it keys.

The sections below record what it took; every slot named was read off a call
site.

<a id="implementation-the-frame-loop-is-the-platforms-because-the-title-registers-nothing"></a>

#### The frame loop is the platform's, because the title registers nothing

A Clet is called through the entry points it registers. A Java title registers
none: it builds a `Card`, pushes it on the display, and **the platform calls that
card's `paint` from then on**. The loop therefore lives here (`java_frame.go`):
once a tick, a `Graphics` over the screen is handed to the card's `paint`, and
the result is presented.

- **A card paints only when it has asked to.** `repaint` is the request, and the
  push is the first. Otherwise a title working on its own thread and repainting
  at the end would be painted mid-update.
- The rectangle of `repaint(x, y, w, h)` is not honoured; the whole card is
  painted. Every local title paints its background first, so the difference is
  work, not pixels.
- `serviceRepaints` paints **now**, on the calling thread, because that thread's
  stack is live. A title driving its frame from its game thread goes through
  there rather than the tick.
- Keys arrive as `keyNotify(type, key)` on the card, with the specification's
  `KEY_PRESSED`/`KEY_RELEASED` and the same key codes as Clet events: one keypad
  serves both kinds of title.

The Graphics object is **the same one every frame**, reset before each paint,
because a title that keeps the one it was handed (the local ones keep it in a
field) needs it to keep working.

<a id="implementation-a-graphics-that-was-never-given-an-alpha-still-draws"></a>

#### A Graphics that was never given an alpha still draws

`setAlpha` is the specification's three-way transparency: zero draws nothing, 255
draws normally, and **every other value counts as 255**, explicitly. There is no
blend, so this platform serves it exactly.

The default is the trap. A Graphics starts opaque, but building its state from
Go's zero value made a fresh one transparent, so a title that never calls
`setAlpha` drew a perfect frame into nothing. One local title did: 2,712
`drawImage` and 356 `fillRect` calls a run, and a black screen.

The rest of the drawing surface is the Clet's: one framebuffer table, one clip and
colour state, one glyph face. `drawImage` uses the same blit, so a picture's
declared transparency is honoured on both paths; `setXORMode` draws the
difference into the same surface; and the anchor constants (`HCENTER`, `RIGHT`,
`BOTTOM`, `VCENTER`, `BASELINE`) move the given point to the corner the drawing
code takes.

<a id="implementation-a-titles-game-loop-is-a-thread-and-a-thread-cannot-be-a-call"></a>

#### A title's game loop is a thread, and a thread cannot be a call

A Clet is called and returns. A Java title starts a thread in `startApp` that
never returns, so **its guest stack must survive across frames** and the platform
must be able to stop and resume it mid-run. That is a goroutine with a private ARM
stack, the arrangement the KTF platform reached for the same reason: the session
grants one step slice and blocks until the thread parks (budget spent, or a
requested `sleep`), so two goroutines never touch guest state at once and a loop
that never returns still advances a slice a tick.

- `Thread.sleep` parks until the deadline **on the guest clock**, so a title runs
  at the speed it was written for. The same call where no guest thread is running
  has nothing to park and moves the clock instead.
- `Thread.start` is a baked vtable slot, named by where it is called: on a
  `Thread(Runnable)` just constructed and stored into a field, past a null check,
  as the last thing `startApp` does. It is the only reading under which
  `startApp` returning leaves the game running.
- A Java title registers no Clet, so the startup that requires one lets it
  through: its initializer *is* its run.

<a id="implementation-synchronized-is-two-slots-and-where-they-sit-says-which-is-which"></a>

#### `synchronized` is two slots, and where they sit says which is which

A synchronized method compiles to `setjmp; if (exception) { A(this); rethrow }
B(this); body; leave the try region; A(this)`. **`B` runs where the protected
body begins and `A` where it ends and again on an exception exit**, so `0x56` is
monitorenter and `0x57` monitorexit. The reverse reading would leave the lock
shut for good.

Threads here run one at a time, so a lock is contended only across a park. Two
consequences, both in `java_thread.go`:

- **A thread holding a monitor is granted another window instead of being
  parked**, keeping a synchronized body indivisible under a scheduler that
  switches only at a park. A bound on renewals keeps a loop inside one from
  holding the frame for ever.
- When the platform's own thread wants a lock held by a guest thread parked
  inside the body, the owner is **run until it lets go**, the wait a handset's
  scheduler would have made.

A thread that ends releases whatever it held, so a failure inside a synchronized
body does not leave the lock shut.

<a id="implementation-the-slots-the-running-titles-settled"></a>

#### The slots the running titles settled

Each was read off its call site; the evidence is in the table it is registered in
(`javaBakedVirtualSlots` and `javaPlatformMethods`).

| Slot | What it is | What says so |
|---|---|---|
| `0x61` | a reference array store, the second one | its first argument answered `[Lr;`, an array of the class its third argument is; nothing at any call site tells it from `0xfa` |
| `0x56`/`0x57` | monitorenter/monitorexit | where each sits in a synchronized method |
| `String` 28 | `substring(II)` | two numbers, the second computed from the first, and a String out |
| `String` 33 | `trim()` | its caller splits a byte array on carriage returns — the title's own text is CRLF — and stores the answer in a `String[]`, so every line but the first arrives with a line feed on it |
| `String` 34 | `toCharArray()` | the answer is indexed with a one-place shift and each element compared against 0x80, which is a run of sixteen-bit units |
| `StringBuffer` 23 | `append(I)` | its argument is a local the loop around it increments and compares against thirteen |
| `Thread` 10 | `start()` | see above |
| `Object` 5 | `notify()` | the last call of a synchronized method that has just written three fields of the receiver; `wait` would have to be inside a loop |
| `InputStream` 13 | `skip(J)` | **two** argument registers, and a loop that steps over fixed-size records |
| `String` 10 | `length()` | its answer is what a text-layout routine compares a position against |
| `StringBuffer` 10 | `length()` | tested against zero right after a loop that appended — the same slot number String's length takes |
| `StringBuffer` 13 | `setLength(I)` | called with zero where a splitter has just stored a token and is starting the next |
| `StringBuffer` 22 | `append(C)` | its argument is loaded with `ldrh` out of a `char[]` |
| `Vector` 15 | `size()` | the answer is taken apart into four bytes and written into a save buffer |
| `Random` 12 | `nextInt()` | the answer goes into a division guarded against `INT_MIN / -1`, so it spans the signed range |

`notify` has nothing to do here: no thread on this platform is waiting on an
object, because a contended monitor retries on its own.

<a id="implementation-an-entry-with-no-name-can-still-be-resolved-by-its-descriptor"></a>

#### An entry with no name can still be resolved by its descriptor

One local title hands over a static entry on `org/kwis/msf/io/Network` with a
**null** name pointer and descriptor `()I`, between the named `disconnect()V` and
`<init>()V` of the same class. Class and descriptor suffice when they leave one
answer: of the methods implemented on that class, take those with that descriptor
and drop every one another entry of the run already names. **One survivor is a
resolution; two are not**, and an ambiguous entry is reported unnamed rather than
guessed.

<a id="implementation-an-abstract-method-is-a-body-and-it-is-not-the-one-to-run"></a>

#### An abstract method is a body, and it is not the one to run

`0x38` is what the compiler emits for a method with no implementation: a
prologue, `mov r0, #0`, a call to `0x38`, and an epilogue; the records mark those
methods `0x0400`, the JVM's `ACC_ABSTRACT`.

Reaching one is a dispatch error, here this platform's. **An override's slot is
often not in the class's own run**: a run holds the methods whose slot the module
needs answered, and a class that only overrides superclass methods needs none.
One title's launcher subclass has an empty virtual run and four overrides, two of
abstract methods, so filling a vtable from the class's own run left the
superclass's abstract stubs in place and `startApp` called one. An override's
slot comes from walking the superclass chain.

<a id="implementation-a-classs-statics-live-in-its-class-object"></a>

#### A class's statics live in its class object

`header + 0x48` is **the class's number of static storage words**, stored in its
class object's data block after the five words the class object uses: the state
halfword the thunks read is word 4, and a static is at `data + 5 + slot`. The
module shows both halves. The compiler bakes the offset where it can
(`ldr r3, [r2, #0x700]` is word 448 of a class declaring 448 static words) and
asks for the rest through the static-field out array, read as `ldrsh` of a slot
followed by `[block + slot*4 + 0x14]`.

Sizing the block by the class object's own five words **overwrote a class object
in two local titles**: a `putstatic` ran off the end of the block into the next
arena allocation, the class object built right after it. The symptom was a class
resolving to a data pointer of 160 several calls later (hence the check that
names it); the cause was one number never read from the record.

Every access in all three modules lands inside `5 .. 4 + count` (253, 40 and 487
distinct words, nothing outside), so the reading is the compiler's, not one
title's coincidence.

**A write watchpoint, `Core.Watch`, found it**: the class object's data word was
watched from creation, and the first store named the instruction,
`str r7, [r3, #0x20]` at a known address, with its value. Everything above
followed from disassembling that instruction. The facility already existed for
cheats but had never been pointed at the platform's own structures.

<a id="implementation-both-doors-of-a-platform-class-answer-with-the-class"></a>

#### Both doors of a platform class answer with the class

This restates [The two entries every class's run opens with](#implementation-the-two-entries-every-classs-run-opens-with):
**neither entry allocates**. The second is reached where the class itself is
wanted, handed to `0x0f` or read for the name at word 2 of its data (what the
array-type call is given). A first reading answered it with an instance, which
surfaced far away: `0x0e` was handed word 2 of an *instance's* data block, zero,
and reported an array of a class named at address zero.

<a id="implementation-arrays"></a>

#### Arrays

**An array is an object like any other**, and the bounds check in front of
`0x23` says what its block holds:

```
ldr   r3, [r6, #8]          ; r6 is the array; r3 is its block
ldr   r2, [r3]              ; the length, at word 0
add   r0, r3, r4, lsl #1    ; the element, two bytes wide here
cmp   r4, r2
blo   ok
bl    0x64/0x23             ; the throw, with the index and the bound
ok:
strh  r1, [r0, #4]          ; the elements start one word in
```

The shift gives the element width, which the platform must match. Four calls
build and use an array:

| Call | What it is |
|---|---|
| `0x0e(dimensions, class, code)` | the array type |
| `0x10(type, length)` | allocate |
| `0x11(type, &lengths, dimensions)` | allocate an array of arrays |
| `0xfa(array, index, value)` | store a reference |

`code` is the JVM's `newarray` numbering (8 byte, 9 short, 10 int) plus its own
1, for an array of the class the second argument names. **The class argument is
meaningful only then**: two local titles pass zero for a primitive and two pass
whatever was in the register.

`0x10` is not a cast (the alternative, since its answer is often dispatched on):
answering with its second argument makes the title immediately read a field of
the number 4. The length argument is a length: literal 3, 20, 50 or 62 at some
call sites, a method call's answer at others.

**A reference store is a call and a primitive store is not**, which is why `0xfa`
exists: a real runtime performs the `ArrayStoreException` check there, which
cannot be done from what the module hands over (an array type carries its
element's name, but an object carries no comparable type). The store is
bounds-checked and let through.

<a id="implementation-something-writes-over-a-class-object"></a>

#### Something writes over a class object

**This was a class's own statics, now fixed**; see [A class's statics live in its
class object](#implementation-a-classs-statics-live-in-its-class-object). The
check that named it stays: nothing handed to the guest is out of its reach, and a
class object is what the guest touches most, so a stray write there fails at the
next resolve rather than where it happened. It costs one read per resolve and
names the class.

The wrong guesses each cost a pass: **array element widths** (allocating every
array at eight bytes an element changes nothing), **`0x10` being a cast** rather
than an allocator, and **a platform instance being too small** (a real defect,
but a different one). More reasoning would not have ruled them out faster; the
watchpoint answered in one run.

<a id="implementation-exceptions-are-setjmp-and-longjmp"></a>

#### Exceptions are `setjmp` and `longjmp`

A try region is two calls, and **the second is not on the Java table**. `0x1f`
answers a buffer that the module hands straight to the C library's `0x32`:

```
bl    0x64/0x1f            ; r0 = the buffer for this region
bl    0x1/0x32             ; setjmp: 0 now, the exception later
cmp   r0, #0
bne   <the handler>
...                        ; the try body
bl    0x64/0x20            ; leave the region
```

The pairing shows it: 58, 57 and 63 call sites of `0x1f` in the three local Java
titles, exactly 58, 57 and 63 of `0x32`, with the first's answer passed to the
second at all 178 sites. The handler reads the thrown object's class from `[r0]`
and tests it against a catch type, so the second return is an object, not a
code.

The family, in full:

| Call | What it is |
|---|---|
| `0x1f()` | open a try region, answer its buffer |
| `0x1/0x32(buffer)` | save the point to come back to; 0 now, the object later |
| `0x20(?)` | leave the region |
| `0x21(object)` | throw what the application built |
| `0x22(object)` | the null check in front of every dereference |
| `0x23(index, length)` | the bounds check in front of every array access |
| `0x25(value)` | the divisor check in front of every division |
| `0x12(class, name)` | the catch test, and `instanceof` |

`0x25` is read off its call site: it is called only when a divisor equals zero,
and the `0x80000000 / -1` special case follows it.

`0x12` takes **two different kinds of argument**: the first is the word before an
object's vtable (a class record's handle for a module-declared class, the class
object for a platform class), and the second a name read from word 2 of another
class object's data. It asks "the class of this object, against the name the
source wrote", answered by walking the chain. A platform class had a zero there
until this pass, because only a module-declared class has a record; its class
object now stands in. (The class-object form of the second argument is in [A
type check is asked with a name or with a class](#implementation-a-type-check-is-asked-with-a-name-or-with-a-class).)

The jump is `Thread.SetContext`: the saved point is a whole guest context, so the
condition flags and instruction set return with the registers. **A frame from an
outer guest call cannot be jumped to**, because restoring registers moves the
guest while this platform's Go stack stays put, so a throw that would cross a
platform call is reported with its class instead.

<a id="implementation-text-streams-and-pictures"></a>

#### Text, streams and pictures

A String's or StringBuffer's contents are kept **on this platform's side**, in a
map keyed by the object the module allocated. The module never reads a String's
words (every use is a call, a static entry or a vtable slot), so there is no
layout to agree on and no gain in writing characters into guest memory. A
StringBuffer works the same way with a different key, and `toString` copies into
a new String, because a title that keeps the answer and appends again must not
see its copy change.

A `String` built from a `byte[]` is decoded as **EUC-KR**, a Korean handset's
default encoding and that of the titles' data files. Undecodable bytes are kept
as themselves, which keeps a byte array of digits readable.

**The C side decodes the same way**, which it once did not. A Clet hands
`MC_grpDrawString` a buffer of handset bytes; rendering them raw put a
codepoint-marked box on screen for each half of each syllable, across one title's
entire six-line notice screen. `MC_grpGetStringWidth` decodes too, because it
measures what `drawString` draws.

**Decoding applies only to text**, not every string read from guest memory. File
names, resource names and property keys are matched by their bytes on both sides,
so decoding one turns a match into a miss: `readCString` stays bytes and
`readCText` is the decoding reader beside it.

**A title reads its own data** with `getClass()`, then `getResourceAsStream(name)`
on the answer, `new byte[available()]`, `read`, and `Image.createImage` on the
bytes, or a `DataInputStream` over the stream for numbers. A wrapper and its
underlying stream are one open stream here, so closing both works (the second
close is a no-op, as the language defines). The resource comes from the
archive's files, as a Clet's resource read does, and the picture goes through the
same decoder into the same framebuffer table, so a Java title and a Clet see one
filesystem and one kind of surface. A resource missing from the archive answers
null, as the specification says and as every call site expects (each null-checks
the answer).

<a id="implementation-a-titles-save-is-the-filesystem-a-clet-writes-into"></a>

#### A title's save is the filesystem a Clet writes into

`org/kwis/msp/io/File` uses the same store, open handles and paths as
`MC_fsOpen`: one filesystem, so a Java title and a Clet of the same game would
see each other's files. Its virtual methods are in the module's class table, so
they arrive by name: `sizeOf`, the `read` and `write` pairs, `write(int)` and
`close`.

**Its mode constants are not the C ones.** The Java class numbers read-only,
write, write-and-truncate and read-write as 1, 2, 3, 4, where `MC_fsOpen` uses 1,
2, 4, 8, so passing one through turns a read-write open into a truncate. They are
translated.

Opening a missing file for reading **throws**, as the specification says the
constructor does and as the original runtime does. It is the first thing a title
does with its save: the `IOException` on a fresh profile is part of a first run,
and the title's handler creates the save. That path depends on the exception
machinery above and was its first end-to-end exercise.

**A file a Java title writes is read back on the next run**, as far as a title
(rather than a test) has carried the round trip. One RPG writes a ten-byte file
during play; restarted on that tree it opens the file, reads all ten bytes and
closes it, twice, before painting anything. `-trace-live io/File` in one run is
the evidence. A progress save is *not* shown: that title's "continue" still says
"there is no saved data", because nothing has reached the save its menus offer.

**How far the driving got.** The title is scripted from boot to its world:
confirm the refused connection, press through the notices, title menu and opening
dialogue, then the tutorial battle, which asks for particular keys (`*` for a
monster's information, `CLR` to come back, `#` to attempt a capture). Assuming
**a key while a message box is up only dismisses the box** (later corrected, see
"The key that dismisses a box is `5`" below), alternating `fire` with the
requested key got through; `fire` alone sat on one message for a thousand ticks,
indistinguishable from a stopped title. The replaying route is one cycle of
`fire`, `*`, `fire`, `clear`, `fire`, `#`, `fire`, `soft1` every sixty ticks, held
fifteen, reaching the village with its quest list open in about seven thousand
ticks; a sweep of the remaining keys is the same script with a different tail.
**`runlgt` now takes `-route`**, the screen-driven script `runktf` has (see
[`cli.md`](../cli.md)), because an eleven-thousand-tick drive written as absolute
`-key` ticks lands somewhere different on every run, while
`wait-idle`/`wait-change` make a step wait for the screen.

In the field, `*` is the quest list, `soft1` the party status, `#` the map, `fire`
takes the selected entry and `clear` leaves it; `0`, `1`, `2`, `3`, `call`,
`hangup` and `7` do nothing. The save the module carries strings for (a "main
menu / help / settings / **save**" menu, and `master.sav` beside the ten-byte
`mastercom.sav` it already writes) is on none of those keys.

**The title's own help screen is the key map, and reading it beats sweeping.**
`txt/strHelpDesc.dat` in the archive lists all three screens: in the field, `#` is
the minimap, `*` the quest list, the clear key the menu and dialogue skip, and
**the send key a quick save**; in a menu the decision key is "EZ" or `5`; in
battle those attack, `*` shows information and `#` attempts a capture. The save is
not a menu entry, which is why menu sweeping never found it; the module's "main
menu / help / settings / save" strings belong to another screen. "EZ" is
`MH_KEY_SOFT3`, missing from this runner's key table; the title offers `5` for it
everywhere, but a title without the alternative would have looked as if it
ignored its own prompt. The send key reaches the card's `keyNotify` (eighteen
deliveries in one run, per the frame loop's per-key log), yet no `master.sav`
appears; the missing piece is whatever the title checks before writing.

**Help text is a static read that generalises.** Several LGT archives ship help
text as `data/StrHelp.*` inside the inner JAR. The `.zt1` form is an eight-byte
header (two little-endian lengths, compressed then uncompressed) and a zlib
stream; the text is EUC-KR with `!cRRGGBB` colour escapes and `!N` line breaks.
Two titles of one family were read this way while a QA pass was sweeping menus
blind: one lists **quick save on the send key** and no save menu; the sibling lists
no save key, so its save is a menu entry or automatic. Two archives in the set
carry no help resource. Where the file exists it is the cheapest evidence in the
project and replaces the expensive half of a QA pass. **It does not say how far
away the key is**: one title's help named its save key, and a route fifteen
thousand ticks and a dozen key presses deep was still in a scripted conversation
where that key advances dialogue.

**The module is the other cheap half.** `strings` over `binary.mod` finds the
filenames a title writes: that title carries `option.sav` (the settings file,
written immediately, so *not* evidence that saving works) and `Save%d.dat`, a
printf format showing a **slot-numbered** progress save. Its absence beside a
present `option.sav` distinguishes "not saved yet" from "the platform did not
write it".

**Two dead ends.** The send key's code is right: `MH_KEY_SEND = -10` is the
specification's, and `keyNotify` takes the same codes as a Clet's event. The
module carries `master.sav` among the UTF-16 constants at `0xe66cc` beside
`mastercom.sav`, but with `-trace-live` reporting every distinct constant built
(below), it appears in a run of two dozen constants built together: a static
table, not the save path running. An earlier reading took the surrounding strings,
one screen's menu (선물하기 / 순위보기 / 게임문의 / CREDIT / 이용정보 / **저장하기**
/ 환경설정 / 도움말 / 메인메뉴), to mean the save entry and filename belong to one
class.

**That reading was wrong: the save filename is a module-wide constant.** The
constants are UTF-16 (a `uint16` length, then the characters) in `.text`, and
`.data` carries a **single global pointer table** at `0x140019c`. `master.sav` is
constant **index 0** and `mastercom.sav` index 1, followed by `수`, `화`, `목`,
`풍`, `광`, `무`, `unit_frm`, `IMG_NUM`, `.mbm`, `/img/`, `PHONENUMBER`, map and NPC
names, in compiler emission order, so **adjacency says nothing about which class
uses a string**.

The function at `0x1834` is the interner:

```
intern(index):
    r1 = [0x140019c + index*4]      ; the constant's address
    r2 = [r1]                       ; its length
    r1 = r1 + 2                     ; its characters
    r3 = 0x1500000 + index*4        ; the .bss slot that memoises the String
    call [0x14046ac](r0, r1, r2, r3)
```

**Nothing reaches it with a `bl`**; a scan of every branch-and-link in `.text`
finds no caller. The module calls its own helpers as it calls the platform's,
through the literal pool:

```
ldr r3, [pc, #..]     ; the helper's address, from this function's pool
mov r0, #<index>      ; the argument — after the load, before the call
mov lr, pc
bx  r3
```

That shape has two traps, each of which produced a confident wrong answer.
**A "who calls X" search must look for the pool entry, not `bl`**: 161 words in
`.text` hold this helper's address, and a branch scan finds none. **The argument
is set between the load and the call**, so a scan taking the nearest *preceding*
`mov r0, #imm` reads an unrelated earlier value; that reading named two call
sites for constant zero, and disassembly showed the real index was `0xd3`, set
after the load.

Done correctly there are 415 intern call sites, 227 with a literal index and 188
computing one. **No literal one asks for constant 0**, so the save filename is
fetched with a computed index, and the next step is the file API rather than
another constant search: the module's import stubs give a title's whole platform
surface without running it, including any open-and-write.

<a id="implementation-every-file-a-java-title-opens-from-the-platform-method-table"></a>

#### Every file a Java title opens, from the platform method table

Chasing the save filename through the constant pool started at the wrong end.
The name is not interned at the call site: it comes out of a virtually
dispatching helper chain (`0xdb520` into `0xdb57c`), which is why no literal
intern index exists.

The right end is the **platform method table**. `javaSVCLoadClasses` fills a
`.bss` table (`0x1500820` in this title), and every platform call is
`ldr ip,[table,#offset]; mov lr,pc; bx ip`, so the offset *is* the method. `0x54`
is `File.<init>`, and every file the title can open is found by one scan for
functions that load the table and then offset `0x54`. There are **eight** in a
megabyte of module:

| function | what |
|---|---|
| `0x169c`, `0x173c` | runtime glue, near the module's entry |
| `0x1e1c8` | |
| `0x79ba0`, `0x7ac84` | a pair, the shape a save and a load take |
| `0xa523c` | |
| `0xda95c` | writes the packaged settings file |
| `0xdad28` | reads it |

Running confirms the last two: the debug log names the call site of every
platform call, so driving the title and reading `from=` addresses identified
them in one run. **`from=` is the return address**, so the call is the
instruction before it.

That narrowed "where in a megabyte" to "which of six functions", and the adjacent
pair answered it. **`0x79ba0` is the save and `0x7ac84` the load**: the first
opens with `mov r2, #4` (write-and-truncate) and the second with `1`
(read-only), the two halves of a round trip.

Neither names a file: both take it from an **array element**, `ldr r1, [r0, #4]`,
behind a bounds check against the length at `[r0]`. The title chooses its file by
*index*, as a slotted save looks underneath, which is why the constant-pool chase
found no caller. The array is filled with computed indexes, consistent with a
loop over the constant table and with the 188 intern sites whose index is not an
immediate.

With `master.sav` constant 0 and `mastercom.sav` constant 1, element 0 would be
the progress save and element 1 the settings the title already round-trips.
**That step is inference**: the array's construction was not traced, only the
shape of the two functions reading it.

For the platform nothing is left to implement. The settings file proves the write
path end to end (opened, ten bytes written, closed, read back next run). Reaching
the save is a driving problem, not a platform gap.

**The save is not in a menu.** Driven into play (the title screen's second entry
loads the packaged save and walks into the village), the in-game menu opens on
`soft1` with five entries: continue, how to play, settings, main menu, quit, and
**no save**. That menu also proves the browser keypad's `Menu` button, which is
this key (`session.md`, "The keypad carries one soft key"); the fifteen-line route
to it (any key for the notice, three `fire`s to the load prompt, `1` to answer)
is kept in `var/routes/` beside the archive, named for this title's in-game menu.

Leaving to the main menu and quitting were both driven and neither writes a byte.
The menu's footer offers `EZ:SELECT` while `soft1` opens it, a detail that cost a
run. Whatever calls `0x79ba0` is therefore a story event or place, not a player
command.

The module counts the candidates without another run. The loader applies no
relocation, so a function's address appears in callers' literal pools as itself:
`0x79ba0` occurs **five** times and `0x7ac84` once. One of each sits in a table at
`0xf7058`/`0xf7074`, twenty-eight bytes apart (a method table, not a call),
leaving **four literal-pool sites for the save and none for the load**.
Disassembling those four is the next, static, step.

**The other Java title answers the question in-game.** Driven to its first
playable screen, its in-game save answers `저장할수 없는 지역입니다!` ("this is not
an area you can save in"), the game's rule about the player's location, not
anything the platform did. Three facts from that run set what a run of either
title costs:

- The save key is `soft3`, the EZ key, and `call` produces the same message: the
  title maps both to one action.
- The opening cutscene is the wall in front of it. It is very long, `skip:#` does
  **not** skip it, and only repeated `fire` walks it: about 6,600 ticks of route
  before the player has control. A key sweep before that sees only dialogue and
  reads as "no key does anything", as an earlier sweep concluded.
- Leaving the opening room for the next area does not change the answer, so the
  whole opening dungeon is a no-save region, and reaching a save point there is a
  play problem rather than a driving trick.

That title also writes its own files in play: `LG_FN0`, five bytes of settings,
and `LG_FN4`, `LG_FN5`, `LG_FN6`, eleven zero bytes each (three empty slots). The
file path proven for one Java title is thus exercised by a second, in-game,
before either has saved progress.

<a id="implementation-a-file-packaged-beside-the-jar-is-a-file-the-title-can-open"></a>

#### A file packaged beside the JAR is a file the title can open

The archive is a zip holding `app_info` and the game's JAR, and only the JAR's
entries were offered to the guest. A handset stored the whole download in the
title's directory, so anything packaged beside the JAR is openable by name; three
local archives ship one:

| what | archive |
|---|---|
| `mastercom.sav`, ten bytes | one Java title, and its coin-edited redistribution |
| `certification` under a Korean directory name | one title |
| `P/kickass`, thirty kilobytes | one title |

The Java title proves it. It asked for its packaged save read-only at startup,
was refused, and **wrote a fresh empty one over it**, so the shipped ten bytes
(holding `0x000f4240`) never reached the game. With packaged files offered it
opens the file it came with, as on the handset.

The JAR wins a name collision, because the JAR is the application and a file
beside it is data the download carried. The JAR itself and `app_info` are not
offered: the loader has already read both, and answering a game with a megabyte of
JAR would be a surprise rather than a resource.

The other two archives do not ask for theirs in an early run, so the effect on
them is unmeasured. The certificate matters most, because a title that checks
one and cannot find it is the shape one KTF title needed `provision` for.

<a id="implementation-what-a-java-title-imports-from-the-stubs-alone"></a>

#### What a Java title imports, from the stubs alone

The stub scan (see "Reading a module without running it") works on Java titles
too; on this one it finds 126 stubs across six tables:

| table | slots | what |
|---|---|---|
| `0x1` | 88 | the C library, plus `0x32`/`0x33` — the OEM framebuffer pointer and width |
| `0x64` | 33 | the Java interface |
| `0x1f8` | 2 | LGT's own, `0x16` and `0x17` |
| `0x1fc`, `0x1ff`, `0x201` | 1 each | slot `0x3` of three tables nothing else names |

A Java title still imports the OEM framebuffer accessors and eighty-eight C
library functions, so its Java calls are not the whole story. The three
single-slot tables are new (no Clet here resolves them), each reached at index 3.

Two smaller findings. The archive ships `mastercom.sav` itself, ten bytes of
`02 00 1e 01 00 0f 42 40 01 01`; `0x000f4240` is 1,000,000, the value the
coin-edited redistributions of this title differ in, so that file is the account
and `master.sav` the progress. And a literal-pool scan over such a module must
disassemble each function from its own prologue: linear disassembly of the whole
`.text` loses sync on the constant pools between functions and reports the loads
it never decoded as absent.

**The key that dismisses a box is `5`, not the one being pressed.** The route
above alternated `fire` with the requested key, assuming any key dismisses a box.
None does: the boxes print `EZ:NEXT` and `EZ:SELECT` in their corner, `EZ` is
`MH_KEY_SOFT3`, and the title offers `5` for it, so `5` dismisses the box and
advances the tutorial while `fire` does nothing. Four keys measured in that state
(`clear`, `call`, `soft2` and `4`) **each left the frame bit-identical**. A screen
answering one key and ignoring four reads as a stopped title, which is what the
earlier sweep saw.

Pressing `5` walks the tutorial (the item box, the dialogue behind it, the party
lesson, the party screen answering `파티넣기`), but the tutorial is a **gate**: it
holds each screen until its instruction is followed, so `CLR:BACK` does not leave
the party screen while the lesson is unfinished, and the field the send key needs
lies behind the remaining lessons. Three routes of eighteen, twenty and thirty
keys got further in and none came out. The next attempt is that route with a tail
that follows the instructions, after reading the module first, since that
settled the same question on the other platform in one pass rather than four
eight-minute runs.

<a id="implementation-what-a-title-has-named"></a>

#### What a title has named

`takeJavaStringConstant` reports each **distinct** string constant the title
builds, once, at debug level (`LGT java names`). A constant is built by the code
holding it, so the set answers "has the title reached the code that names this"
(which file it will open, which screen it will draw) without disassembly. Only
the first build is reported, because one title rebuilds the same constant every
frame and a line per build buries everything else.

Its limit is the one above: a class whose constants are all built together in its
static initializer says nothing about which of its methods ran. A name appearing
is a floor, not a proof.

<a id="implementation-a-caught-throw-is-not-a-return"></a>

#### A caught throw is not a return

A throwing platform method has no result, and **nothing must be written as its
answer**: the jump has already put the exception in the answer register and the
whole context belongs to the handler. Writing a result over it made a caught
failure read as success: the title took the *normal* path out of its `try`,
believing an unopened file was open, and failed two hundred instructions later.
The throw therefore travels back as a sentinel error that every caller servicing
a platform call recognises.

<a id="implementation-what-a-title-links-against-from-the-modules-own-metadata"></a>

#### What a title links against, from the module's own metadata

A Java title's demand on this platform is a set, not a sequence of failures, and
the module carries it: the load call hands over five member tables and a class
table saying which class owns which run of each. This is the LGT counterpart of
reading a KTF module's string pool, and it replaces one run per member:
`TestLocalAPIDemandProbe` lists every member a title names and marks those this
platform serves:

```sh
WFEATURE_API_DEMAND_ARCHIVE=<abs path> WFEATURE_API_DEMAND_PREFIX=lwc \
    go test ./internal/platform/lgt -run TestLocalAPIDemandProbe -v
# MISSING static     72 org/kwis/msp/lwc/TextFieldComponent.<init>(Ljava/lang/String;I)V
# MISSING virtual    24 org/kwis/msp/lwc/TextFieldComponent.setString(Ljava/lang/String;)V
# served  static     77 org/kwis/msp/lwc/ShellComponent.<init>(IIII)V
```

**It cannot say which class owns an instance field.** The field table is shared
with the application's classes and entries carry no owner, so those come back
unattributed. That still answers "does this title read a platform field at all",
the question the section below turned on.

<a id="implementation-the-lwc-widgets-and-the-field-a-title-reads-instead-of-asking"></a>

#### The lwc widgets, and the field a title reads instead of asking

Two titles reach a text field, one asking the player for a name, the other for a
message. The demand is the specification's TextComponent family: a field or box
built with a string and a constraint, `getString`, `setString`, `setMaxLength`,
the `insert` and `delete` a handset's input method drives, the shell holding
them, and the `InputMethodHandler` a text component owns. The widget layer stores
field contents and constraints and does not draw the toolkit; its native text
input support is in [Native text input boundary](#followup-native-text-input-boundary).

**A key offered to a widget is answered false**, because a widget taking it would
take it from the card, the only thing drawing, and the player would type into
something invisible.

**The field took the work.** `TextComponent.imHandler` is protected on the
handset, so a title reads the automaton off the component rather than
constructing one, and the module asks for that field's number in the same table
as its own fields' numbers. This platform never answered that table: a platform
class's instance state lives on the Go side and is reached through methods, so
the class table's field run was skipped. An unanswered entry leaves zero in the
module's array, the read lands on the object's first word, and the title got an
unchecked null: `java/lang/NullPointerException` in a thread whose death showed as
a `Now Loading` screen for ever.

Three pieces close it. `layoutPlatformFields` numbers each platform class's own
instance fields above those its superclasses took and answers `FieldsOut`; a
platform class's object block is sized to hold them, since a block sized to
nothing puts the module's read past the arena allocation; and a text component's
constructor builds the handler and writes it into the word. **All three are
needed**: the number without the block writes outside the allocation, and the
block without the constructor holds a zero the title reads as null.

<a id="implementation-a-file-written-and-never-closed"></a>

#### A file written and never closed

An open file's bytes are kept here and persisted on close, which suits a title
that closes what it opened. One does not: it writes the single byte marking its
first run as over and calls `MC_knlExit` on the next line. The file was created
on disk at zero bytes, the second run read it empty, and the title showed its
first-run notice again on every run, indistinguishable from a title stuck at its
splash.

`flushOpenFiles` persists every dirty open file at the two other moments a write
becomes durable: the guest's ending and the session's. It costs nothing per
write, unlike a flush per write, under which a title filling a save a byte at a
time would rewrite the whole file per byte.

<a id="implementation-one-surface-per-picture-named-or-not"></a>

#### One surface per picture, named or not

A picture loaded from a resource is immutable (the specification says only a copy
can be drawn into), so two Images of the same name are the same pixels. Decoding
a second set costs a surface, and **at that stage nothing reclaimed one**: the
Java path had no collector yet, and on a handset the language releases an Image.
One title reloads its sprite sheets from inside `paint`, 879 loads of a handful of
names in two thousand ticks, and the surface region filled and ended the run.

`newSharedJavaImage` keeps the surface under its source, the resource name for
`Image.createImage(String)`. The object is still new: sharing pixels is
unobservable, while sharing the object would make `==` answer true where the
language says nothing.

**The byte form is cached the same way, keyed by the bytes' digest.** The
objection, that two matching arrays are not the same picture as two loads of one
name are, applies to sharing the *object*, which nothing here does: each call
answers its own Image, and drawing into one takes a private copy first
(`unshareDecodedSurface`, as for the named form). What is shared is the decode,
and one decode per picture is right however the title asked.

**A surface whose Image nothing holds is still not reclaimed**; the cache makes
that survivable by turning the unbounded case (the same picture decoded
repeatedly inside `paint`, the actual failure) into one surface.

**Measured over the local Java set, nothing climbs.** Counting cache misses
(distinct pictures, exactly a title's surface cost) across all fourteen archives
at three thousand ticks: eight decode fewer than fifty, four decode none, and the
largest decodes 517. That one is flat: six thousand ticks decode the same 517, so
it loads its sprite set once and the cache serves every repeat. A title building
a genuinely *new* picture every frame would still climb; closing that required
the collector described later in this document, based on the other platform's
(`internal/platform/ktf/collect.go`, and `docs/ktf.md`'s "A screen that only waits
still allocates" for what it bought). No local title does that.

<a id="implementation-an-exception-nothing-catches-ends-the-callback-not-the-session"></a>

#### An exception nothing catches ends the callback, not the session

This is the other side of the try region: the search finds nothing armed. A throw
reaching the platform's own frame used to end the run, which two local titles
show is wrong.

Both leave a scene the same way (stop the sound, drop the outgoing scene's
pictures by storing null into their own array, request a collection), and both
are painted once more before the next scene's pictures load. That frame reaches
`drawImage` with a null, in one paint, from a card whose loop is elsewhere. The
language says an uncaught exception ends the *thread*; a handset's event loop
ends the callback and calls the next. Ending the session stopped both titles on a
frame they were about to discard.

`Client.absorbUncaughtCallback` applies that rule where a callback is entered: a
card's `paint` and `keyNotify` through `callJavaCardMethod`, and a guest thread's
`run`. **Only an exception the application threw is absorbed**
(`javaUncaughtThrow`), so an unserved slot, a fault, a Host-imposed limit and a
guest exit still stop the run; the type separates those from the one case where
the guest behaved as written. It is counted, not swallowed:
`Session.UncaughtCallbacks` carries the count and the first message into the CLI
summary's `uncaught` key and the browser session report, so a title failing every
paint is visible as such. The other platform's counterpart and full reasoning are
in [`ktf.md`](ktf.md#implementation-ktf-platform-implementation-status), "who
catches what a callback threw".

**A null image is the language's exception, not a platform refusal.**
`javaImageSurface` answers the surface an object stands for and reports an object
this platform never issued as a defect, but zero is the missing object, not a
wrong one, and the specification gives `drawImage` a `NullPointerException` for
exactly that argument. `javaImageArgument` makes the distinction: null throws
what the handset threw, and every other refusal stays a refusal. Neither title
moves without both halves: the throw alone still ends the session, and the absorb
alone has nothing of the right type to absorb.

<a id="implementation-the-string-pair-and-the-two-slots-that-answer-them"></a>

#### The String pair, and the two slots that answer them

`0xe1` and `0xe2` take **no argument at any of their 254 call sites**, and each
answer goes straight to one call: `0xe1`'s to the allocator, followed by a
`java/lang/String` constructor; `0xe2`'s to the array allocation, whose elements
are stored through the reference store `0xfa` and read back from fields declared
`[Ljava/lang/String;`. They are `String` and `String[]`, what a compiler would
hard-wire: a Jlet is entered with one, and every string constant is the other.

`0x9` **answers the string it makes** and checks the cache slot it is passed. The
module's constant helper is only this call (look up the pool entry, pass units,
length and slot, return the answer), so answering zero makes every constant null.
It showed as three constants appended into a `StringBuffer`: a resource name of
`"nullnullnull"`, four calls from the wrong constant.

<a id="implementation-what-game-names-and-why-it-is-not-in-the-class-list"></a>

#### What `"Game"` names, and why it is not in the class list

It is a class. Every local Java title has a class record named exactly the
module's `argv[0]` (`"Game"` in two, a two-letter name in the third), extending a
class that extends `org/kwis/msp/lcdui/Jlet`. `Main.main({"Game", "", "true",
"true"})` asks the launcher to instantiate the named Jlet and start it.

**The launcher class is not in the list `0x07` hands over**, nor are the two
abstract classes between it and `Jlet`, which hid it. The list holds the classes
the module resolves at run time; a class the module never writes `new` for has no
reason to be there, and the launcher class is exactly the one the *platform*
constructs. Reading the list as the whole application made the class look absent.

All are in the image, end to end in `.data`, and a record is recognisable alone:
the word at `handle + 8` names its header, which ends with the `0xfffffffe`
sentinel. A scan for that pair finds 20, 23 and 25 records in the three local
titles with no false positives; `findJavaClassRecord` uses it to find a class the
module never handed over. Every local Java archive now reports its launcher
class, superclass chain and `startApp` address from a real run rather than from
the ELF.

The scan does not skip executable sections, deliberately: **a module marks the
section holding its class records executable**, so skipping by that flag finds
nothing. The two identifying words are specific enough that walking a megabyte of
ARM finds no false record.

The Jlet subclass carries `startApp([Ljava/lang/String;)V`, `pauseApp`,
`resumeApp`, `destroyApp` and `run()V` as ordinary method records with their own
code addresses, so **once the object exists, starting the application is a call
to an address the record holds**. `JletWrapper`'s static-method entries are the
platform's side of the same lifecycle, which is why no call site for them was
ever found.

`runJavaLauncher` does exactly that: prepare the class, allocate an instance,
call `<init>`, call `startApp`. The `String[]` argument is passed as null because
nothing builds an array yet; a title that reads it will show it through the null
throw, a better place to find out than a guessed array. Nothing yet pauses,
resumes or destroys the application, or drives the event queue a Jlet expects.

**Calling guest code from a platform call runs on the *running* thread**, not the
platform's own; see `callOn` and [The slot that runs a function](#implementation-the-slot-that-runs-a-function)
for what ignoring it cost.

The launcher gave a second rule: **a class's object is built before its layout**.
Laying a class out runs the module's code, which resolves classes, including the
one being laid out; answering that re-entry with a half-built class returns a
null that surfaces at an allocation several calls later.

<a id="implementation-the-application-asks-the-platform-for-itself"></a>

#### The application asks the platform for itself

The object the launcher builds is the one thing a title cannot reach alone. A
`Card`'s `paint` and a listener's callback are entered with no reference to the
application, so a title needing one asks by name through a *static* call, which
is why the launcher's local variable was not enough. One title's title screen
calls `Jlet.getCurrentJlet()Lorg/kwis/msp/lcdui/Jlet;` from inside
`GameCanvas.paint` on the first key press there; until the platform kept the
object, that stopped the title at an unimplemented static entry, the screen
before its opening.

The launcher therefore records the instance it allocated before `<init>` runs,
since the constructor is the first guest code that can ask. The specification has
two spellings, `getActiveJlet` ("the Jlet at the top") and `getCurrentJlet` ("the
Jlet running now"), distinct on a handset running several programs. **This one
runs a single program**, so both answer the same object, as the KTF runtime
already does for the pair. Before the launcher builds one the answer is null, the
correct answer rather than a stop, since nothing has started.

The call site type-checks the result (it reads the vtable word before slot 0, the
class, and hands it to the module's comparison), so a wrong object would not fail
at the call. That is why a slot is answered with the platform's own record of
what it handed out rather than anything reconstructed.

<a id="implementation-reading-a-module-without-running-it"></a>

#### Reading a module without running it

The findings above came out of the ELF rather than a run, because the metadata
is all in `.data` and `.text` at the addresses the module will use. What that
takes: parse the section table, resolve an address to a file offset, and
disassemble with `capstone` in ARM mode — the AOT bodies and the startup glue
are ARM, not the Thumb the entry point is in.

Three things make it fast. The module's import stubs are a run of four-word
records in `.data`, each `str lr,[sp,#-4]!; bl <resolver>; <table>; <index>`
before resolution, so **a scan for that first word yields the complete set of
platform functions a title uses** (41 slots in one local title), with the two
following words identifying each stub.

A literal-pool search finds call sites: one index of every `ldr rN, [pc, #imm]` in
`.text` against the word it loads answers both directions. Asked for a stub's
address it gives every call site of that slot (how `0x0f`'s 172 allocations and
`0x22`'s 4083 null checks were told from `0x11`'s four); asked for a `.bss`
address it gives every use of an out array (how each array's element size and
meaning was settled). **The counts alone rank the work**: thousands of sites
means every dereference, a single site means startup.

The layout tables can be found rather than followed. A class record identifies
itself (`[handle + 8] == handle - 0x4c`, and `0xfffffffe` at `header + 0x44`), and
the application's layout table is the only run of 24-byte entries whose first
words all name such records. Neither needs a run.

<a id="implementation-what-a-sweep-of-a-94-archive-set-asked-for"></a>

### What a sweep of a 94-archive set asked for

A local set of 94 archives was driven from a fresh save in the other WIPI
platform's five passes: a stepped probe, a three-thousand-tick run, a key script
over every title whose screen had not changed, a timed four-hundred-tick run over
unfinished ones, and a second key-script run against the first run's save tree.
55 reach a screen that keeps changing or answers a key, 24 stop with an error,
and the rest sit still or run too slowly to judge. **Ten of the 55 needed the
second run**, holding a "please start the game again" notice on the first. What
stops the 24:

**Fifteen are Java titles, stopping in two places.** Seven never got past the
class list: `java slot 0x7` refused with "class handle 0x0 is not a word-aligned
address". **A null slot is a class the module named and never materialized,
because it is an interface**: interface dispatch goes by the receiver, so no
record is loaded. The slot keeps its place (the string constants start where the
counted handles end), and all seven then reach the other eight's wall.

The other eight ran their launcher and stopped on an unimplemented slot: five on
`Display.getDockedCard()Lorg/kwis/msp/lcdui/Card;` at vtable slot 12 (dispatched
from the launcher), one on `Vector.<init>(II)V`, one on
`Clip.<init>(Ljava/lang/String;)V`, and one on `DataInputStream` vtable slot 28 of
64 with an `InputStream` argument. All but the last are now answered, with the
next round's `Font.getFont(III)`, `charWidth(C)` and `StringBuffer.<init>(I)`.
Capacity and growth arguments are hints for storage that grows on demand, and any
named face, style and size answer the one font: refusing a bold face stops a
title, answering the plain one draws its text.

**`org/kwis/msp/db/DataBase` holds four titles' saves**, which did not start
without it. Each database is one container in the store a Clet writes files into,
because the specification puts records in the platform's persistent area and one
save directory per game beats two stores that can disagree. The container format
is this runtime's own (no handset's is read): a header naming record size and
count, then length-prefixed records, a deleted record keeping its slot under a
length no real record reaches, so record identifiers stay stable and reused as
specified. **The store cannot be listed** (a browser Host implements the same
key-value contract, without enumeration), so `listDataBases` reads one more entry
of names, rewritten by every create and delete.

The fifteen now ask for a longer tail rather than one wall:
`java/io/ByteArrayOutputStream` and `java/io/InputStreamReader`,
`java/util/Calendar.getInstance`, `java/util/Stack`, and four `java/lang/String`
and `java/util/Vector` vtable slots (11, 19 and 26 on String, 28 on Vector) whose
call sites had not been read. Two of the fifteen are past the platform: one
reaches its own `paint`, one the tick loop.

<a id="implementation-answering-that-tail-and-what-a-tail-looks-like-when-it-is-being-answered"></a>

#### Answering that tail, and what a tail looks like when it is being answered

**A title that stops on a missing member does not stop once**: answered, it stops
a few hundred instructions later on the next member of the same shape (a title
reading its save wants `readFully`, `readByte`, `readInt`, then the wrapping sink
and stream), so the unit of work is a class's run, not a slot. **Five now run
three thousand ticks without error, from none, and six moved to a later wall**,
each a named slot. The other five are unchanged: three never had a missing member
(two the launcher cannot start, one whose thread class declares no `run`), and two
ask for something this pass did not reach.

What went in, each placed by the numbering rule and checked against its call
site:

- **`java/lang/String` 11, 14, 16, 19, 26 and 29**: `charAt`, `getBytes()`,
  `compareTo`, `startsWith(String)`, `indexOf(String, int)` and `concat`.
  `getBytes` uses the encoding the String constructor decodes with, so the pair
  round-trips on a handset whose default encoding is not UTF-8.
- **`java/lang/StringBuffer` 17 and 27**: `append(Object)` and `delete`. The
  first cannot call `toString` on a guest object and names the object by its
  class instead, which suits the one local shape reaching it (an exception
  written into a line).
- **`java/util/Vector` 28 and `java/util/Stack`**: `insertElementAt`, and a Stack
  related to Vector so its four own methods reach Vector's slots.
- **`java/io/DataInputStream` 20, 23 and 28**: `readFully([BII)`, `readByte` and
  `readInt`. `readFully` differs from the adjacent `read([BII)` in one way that
  matters: it fills the array or reports, where the other answers a short count
  and leaves the loop to the caller.
- **`java/io/ByteArrayOutputStream` and `java/io/DataOutputStream`**: the sink a
  title builds a record in, and the numbers written into it. A wrapper is not a
  second sink; it holds the handle of the one it was built on, so writing through
  it and reading the sink's bytes agree. `writeUTF` matches the read side's
  `readUTF`, as `getBytes` matches the constructor.
- **`java/util/Calendar`, `java/util/Date` and `java/util/TimeZone`**: the date a
  title shows, from the same clock every other call reads. A calendar splits that
  instant into the components `get(int)` names; there is one zone, so every
  calendar answers it.
- **`java/util/Random.<init>(J)` and slot 10**: a hand-seeded generator, a title
  asking for a repeatable run.
- **`org/kwis/msp/io/FileSystem.available()` and the two-argument `exists`**: the
  room left, and the form naming a directory. A title has one directory here, so
  the flag chooses nothing.

Re-driving the whole set from a fresh save with the binaries before and after
this pass: five stopped titles finish three thousand ticks, six moved on, and
**nothing that finished before stopped**. A sweep cutting a slow title must cut it
identically in both runs, and even then cut rows are not results: of the five
this sweep cut, three finish alone (in seventy-nine, a hundred and eight and two
hundred and twenty-one seconds), one reaches the instruction budget given long
enough, and one still runs after seven minutes. **A row the harness cut is a row
nobody measured**; counting it as a failure mixes the slow into the broken.

Remaining at this point (each answered in a later section):

- **one slot left unimplemented on purpose** (see "The declaration order is the
  class library's, not the listing's");
- **seven stops on unimplemented platform slots**: four stdlib (`0x3f9`, `0x426`,
  and `0x404` twice) and three WIPI-C (`0x6f`, `0x19f`, `0x44c`), two of the
  WIPI-C ones from titles whose whole subject is sound;
- **a string that never terminates** (stdlib `0x411` walking past 4096 bytes) and
  **a fault on unmapped guest memory at `0x8` in the module entry point**, before
  any platform call;
- **seven titles holding a screen through two runs**, six explained by reading
  the log with the frame: one stops at tick 201 on WIPI-C slot `0x197`, one faults
  at tick 2 inside the pixel operation slot `0xd3` calls, one Java title stops on a
  `java/io/ByteArrayOutputStream` slot, two hold their authentication screen
  (인증중 on the frame, `Connect CB Error [-1]` in the log), and one paints its own
  "the save failed, the game is closing" dialog over an empty save tree. Only a
  black screen with an empty log is unexplained. **A held screen is a log to read,
  not only a key to press.**
- **six titles far slower than the handset** (fourteen to fifty-four seconds for
  four hundred ticks) and **two that do not finish four hundred ticks**: one
  killed after five minutes, one ending on the three-billion-instruction budget
  inside `startClet` after four minutes.

<a id="implementation-the-declaration-order-is-the-class-librarys-not-the-listings"></a>

#### The declaration order is the class library's, not the listing's

The rule in `java_api.go` (own methods from slot 10, overrides keep the
superclass's slot) left one question: **in what order?** The specification's
per-class pages list methods alphabetically, fitting no anchor; the same site's
CLDC pages print each class in **declaration order**, fitting every anchor across
six classes:

- `java/util/Vector`: `copyInto`, `trimToSize`, `ensureCapacity`, `setSize`,
  `capacity`, `size`, `isEmpty`, `elements`, `contains`, the two `indexOf`, the
  two `lastIndexOf`, `elementAt`, `firstElement`, `lastElement`, `setElementAt`,
  `removeElementAt`, `insertElementAt`, `addElement`, `removeElement`,
  `removeAllElements` puts `size` at 15, `contains` at 18, `indexOf` at 19,
  `elementAt` at 23, `firstElement` at 24, `removeElementAt` at 27,
  `insertElementAt` at 28 and `addElement` at 29. **All eight were already
  settled from their call sites**, none placed by this order; they are the check.
- `java/lang/String` fits at 11, 14, 16, 19, 26, 28, 29, 33 and 34,
  `java/io/DataInputStream` at 20, 23, 25 and 28, `java/io/InputStream` at 10 to
  15, `java/lang/Object` at 1 and 5, and `java/lang/Thread` at 10.
- **`toString` lets a run end higher than its method count.** It overrides
  Object's and takes Object's slot, so Vector's twenty-three declared methods end
  at 31 rather than 32, putting `java/util/Stack`'s own five (`push`, `pop`,
  `peek`, `empty`, `search`) at 32 upward, where a local title dispatches its push
  and pop.

With the order settled, a slot with one anchor becomes arithmetic the call site
then confirms. Nine slots went in that way, each checked against what its site
passes and does with the answer: `Thread.setPriority(int)` at 14 (called on
`currentThread()` with a literal ten, answer dropped; the class's only method
taking an argument), `Object.wait()` at 9 and `wait(long)` at 7 (dispatched on the
object locked by the previous instruction), `Vector.isEmpty` at 16 and
`removeAllElements` at 31, `String.indexOf(int)` at 21 and `indexOf(int, int)` at
22 (its site looks for `U+3000`, the ideographic space),
`DataInputStream.readFully([B)` at 19, `readBoolean` at 22, `readUnsignedByte` at
24 and `readChar` at 27, and `InputStreamReader.read(char[])` at 11 with `close`
at 18.

**One slot disagrees with the rule, and the site wins.** One title dispatches
`java/util/Calendar` slot 29 with a `Date` three instructions after
`Calendar.getInstance()` and building the `Date`; `setTime` is the class's only
method taking one. The numbering puts `setTime` at 11, eight short of a run
ending at 21, so either that module was compiled against a fuller Calendar or the
run does not start where it appears to; nothing read says which. It is
implemented as `setTime`, and the disagreement is recorded: a rule never allowed
to be wrong is not evidence.

<a id="implementation-what-that-pass-moved-and-what-it-cost"></a>

#### What that pass moved, and what it cost

Six stopped titles now run three thousand ticks without error, and **five draw
their own screen**: four with the frame changing, one holding its "press any key"
notice and reaching its title screen under a key script. The sixth finishes every
tick and paints black, a different case recorded below. Beyond the slots above,
the six needed:

- **`Display.callSerially(Runnable)`**, through which one title hands over its
  whole game as the last act of `startApp`. The Runnable runs at the top of the
  next tick (after events, before the frame), not inside the call, which would
  leave start-up on the stack under the game loop for the whole session
  (`ServiceJavaSerialCalls` in `java_frame.go`).
- **`Object.wait` must release the lock.** With no wait queue (the language needs
  none: a wait may return unnotified and correct callers re-test), it hands over
  one turn, releasing and retaking the whole monitor depth, because a
  `synchronized` method calling another on the same object waits from inside two
  and keeping the monitor would block the thread that changes the condition.
- **`closeDataBase` on an already closed database is ignored**, as specified; one
  title closes at the end of its save and again leaving the calling routine. The
  object stays in the table marked closed, so a call on an object this platform
  never issued still stops the title.
- **`Graphics.getFont`, `reset`, `clipRect` and `setPixel`.** `reset` is absent
  from the specification's listing but is the call titles on the two other
  platforms make on their kept Graphics, with the same meaning. `clipRect` only
  narrows, so with no clip set it intersects from the surface bounds.
- **`java/io/InputStreamReader` and `java/io/ByteArrayInputStream`** stand for the
  underlying stream rather than holding a copy, so a read through either moves
  the one cursor. The reader decodes with the handset's encoding and keeps its
  cursor in **bytes**, taking one character's worth at a time.
- **Two interface functions are a pair.** `0xfd` takes four registers where the
  adjacent reference store takes three, the extra being a long's high half (its
  site clears a `long[]` in a loop). `0x5b` is the matching load (array and index,
  answered in a long's register pair), the whole body of a one-line accessor.

**Still ahead:** one title reaches the unseen interface function `0x64`, which
looked like a type test; it is a method lookup, see [What interface function
`0x64` is, read off its call site](#implementation-what-interface-function-0x64-is-read-off-its-call-site).

**One finishes every tick with nothing on screen.** All three thousand flushed
frames are the same black while the log shows drawing: a hundred and twenty-eight
`drawImage`, two hundred and six `drawLine` and five `fillRoundRect` on the screen
Graphics, with its Card as its own Runnable handed back to `callSerially` every
frame. Making `clipRect` a no-op changes nothing, so the clip is not the cause.
**A title that finishes its ticks is not a title that works**; counting it as
answered because it stopped failing is the error the cut-rows rule exists to
prevent.

<a id="implementation-four-c-library-slots-named-by-what-their-callers-do-with-them"></a>

#### Four C library slots, named by what their callers do with them

The four stdlib slots that pass left unnamed are now named, none by counting off
the specification's list. That list is the right first question (the C library's
sectioning puts `sprintf` at `0x3f7` and `strcpy` at `0x405`), but this table is
known to depart from it, so a position is a hypothesis and the call site the
evidence.

- **`0x404` is `srand` and `0x403` is `rand`.** Two titles stop on `0x404`, both
  one call after `time` with `time`'s answer in the first register:
  `srand(time(NULL))`, the idiom, spelled identically by two independent titles.
  Seeding carries the first title through three thousand ticks and the second as
  far as `0x403`, called with no arguments set up and its answer fed straight into
  arithmetic: `rand`. Implementing the first found the second, as with class
  members.
- **`0x3f9` is `vsprintf`.** Its caller passes a destination buffer, a format just
  assembled on its own stack (a file name), and a third pointer into the frame
  above, which is a `va_list` on this ABI. The number agrees: `sprintf`, `sscanf`
  and `vsprintf` are the three `<stdio.h>` functions a handset keeps once those
  taking a `FILE *` are dropped, and counting from `sprintf` at `0x3f7` lands
  `atof` and `atoi` where the table already had `atoi`. The renderer is
  `sprintf`'s, with the argument walk changed from registers-then-stack to a
  cursor through guest memory.
- **`0x426` is `malloc`.** Its number says nothing (it sits past `localtime` with
  unnamed slots either side); its caller says everything. A title calls it with
  140, 68 and 17, each time then `memset`ting exactly that many bytes at the
  result. Handing the size back makes the `memset` write to unmapped address 140;
  answering null sends the module into its own data and a nonexistent supervisor
  call. It is served from the `MC_knlAlloc` heap so a later free through either
  door reaches the same block, and it does not zero memory: the caller's own
  `memset` shows the handset's did not either.

Each of the four was one title's whole wall. All four titles now finish three
thousand ticks and draw their own screens: two carrier notice screens, one
animating usage notice, and one reaching its full title art on the second run
once its save exists. **A C library stop is this platform's cheapest wall**: the
call site is a few instructions, the contract is standard, and what the caller
does next names the function when the number does not.

<a id="implementation-four-platform-slots-the-specifications-own-ordering-named"></a>

#### Four platform slots the specification's own ordering named

Where the C library's numbering had to be argued from call sites, the WIPI C
blocks follow the specification's order exactly, and three of the four slots came
from checking that rather than from disassembly.

- **`0x6f` is `MC_knlGetProgramName(nameBuf, bufSize)`.** Counting the kernel
  block from `MC_knlPrintk` at `0x64` (printk, sprintk, getExecNames, execute,
  exit, programStop, getCurProgramID, getParentProgramID, getAppManagerID,
  getProgramInfo, getAccessLevel, getProgramName) puts every existing entry where
  it already was, through `exit` at `0x68`, `getCurProgramID` at `0x6a`, `alloc`
  at `0x75` and the timers at `0x7a`-`0x7c`. The call site agrees: sixteen zeroed
  bytes of the title's stack, passed with the length. The name is the
  descriptor's, in the handset's encoding; a too-small buffer gets `M_E_SHORTBUF`
  with the buffer untouched, as the specification says and as a caller that
  cannot tell a truncation from a name needs.
- **`0x19f` is `MC_fsTell(fd)`.** The same count over the file block from
  `MC_fsOpen` at `0x190` lands `isExist` at `0x1a0`, where it already was,
  fourteen entries later. The call site agrees: the title seeks to the end and
  asks one instruction later, as a program without a size call measures a file.
- **`0x197` is `MC_fsRename(oldname, newname, accessLevel)`** by the same count:
  the seventh file-block entry, between `MC_fsRemove` at `0x196` and `MC_fsMkDir`
  at `0x198`, both already where the count puts them. The store has no rename (a
  path is a save key), so the new name is written with the old one's bytes and
  the old name removed. The specification's two refusals make it a rename rather
  than a copy, both leaving the store untouched: a missing source is
  `M_E_NOENT`, an existing destination `M_E_EXIST`. **One title depends on it**:
  it writes its save under a temporary name and renames it into place, and
  without the slot stopped at tick 201 on a screen asking the player to restart
  the handset.
- **`0x44c` is `MC_dbListDataBases(buf, len)`**, named by format rather than
  number. The caller passes four freshly zeroed kilobytes, then walks the result
  as names separated by one NUL and terminated by two, the layout the
  specification gives for this function alone in the C API. That puts the
  database block's base at `0x440`. **The rest of the block is unimplemented**, so
  no title here has opened a C database and the correct answer is none: zero, and
  the empty list's two NULs. That is a documented success, not a stub, and stops
  being the whole answer once `MC_dbOpenDataBase` is served.

**Nothing in the local set asks for the rest of the database block**, and it stays
unimplemented on that measurement; see "What a module resolves, and when" below
for the measurement and its worth.

**The value of the block ordering is not three slots** but that a slot's number
can be checked against the specification before disassembly, and a block whose
known entries all land where the count puts them can be trusted for the missing
ones. The C library's numbering fails that check (hence the caller arguments
above); the WIPI C blocks pass it.

<a id="implementation-the-fourth-round-where-a-class-record-says-its-members-are"></a>

#### The fourth round: where a class record says its members are

Five of the eleven remaining titles shared one cause: one word read from the
wrong place.

**A class record's member runs are located by its header, not at a fixed offset
from its handle.** The platform's reading was the shape the first records
happened to have: the field count at `handle+0x0c`, that many five-word field
records, the method count, then that many seven-word method records. Most records
have that shape, but *where each run starts* is in the header, at `header+0x3c`
for fields and `header+0x38` for methods, and a class declaring no fields carries
a zero field pointer and starts its method run where the field count would be.

The old reading took the first method record's owner word for a field count and
walked method records with a field's stride. The walk failed on the second
member, the record was abandoned, and the class declared nothing. **Callers saw an
empty class, not a parse failure**: the launcher found no constructor for a class
that has one, a thread class declared no `run`, and a title's own methods were
invisible to the vtable builder. Reading both runs from the header fixes all of
it, checked against every local module: across the 94-archive set's class
records the two readings agree except on the twelve records the old one
abandoned (in eight titles), and no record succeeds under the old reading and
fails under the new. One record neither reading reached before is now read: a
class whose header carries both a compiler-built dispatch table *and* a method
run, where the old reader stopped at the pool pointer.

That change moved five titles, each then needing something of its own:

- **A launcher's class name arrives in source form.** `argv[0]` is a name written
  into a descriptor, so a Jlet in a package arrives as `pkg.GameMain` while every
  class record spells it `pkg/GameMain`. A class name cannot contain `.`, so
  nothing else can be meant.
- **`Graphics.setFont(Font)` is accepted and changes nothing**, for the reason
  `Font.getFont` answers the default face: there is one face, so the set font is
  the one the title would have drawn with. Refusing it costs the screen the text
  was on.
- **`java/lang/String` slot 27 is `substring(int)`** and
  **`java/io/DataInputStream` slot 32 is `readUTF()`**, both from declaration
  order: the first sits just before the two-bound `substring` at 28, the second is
  the last non-static method of a run whose `readInt` is at 28.
- **`FileSystem.isFile` cannot be answered differently from `exists`.** Nothing
  here creates a directory, so an existing name is a file's. (Superseded
  2026-10-22: `exists` now also answers for a directory — the root, one `mkdir`
  made, or a name with something below it — and `isFile` for a file alone; see
  [The storage contract](#lgt-storage-contract-2026-10-22).)

<a id="implementation-a-title-recolours-a-picture-and-does-not-fix-its-checksum"></a>

#### A title recolours a picture and does not fix its checksum

One title stopped on `png: invalid checksum` from a 171-byte picture whose every
other chunk verified. The cause is a sprite sheet recoloured at run time: the game
keeps one encoded picture in a `byte[]`, writes new entries over its `PLTE` chunk,
and hands the array to `Image.createImage([BII)`. A handset's decoder never checks
the CRC-32 beside the chunk, so the edit is free there; Go's decoder checks it and
refuses the whole picture over four bytes the title never meant to be read.

`wipic.DecodeStandard`, where all three platforms' image routers now end, retries
**only after a decode has failed**, over a PNG whose chunk checksums it can
recompute. A picture that decodes keeps every check, and damage beyond a checksum
stays damage: rewriting a CRC does not make a broken deflate stream inflate.

<a id="implementation-systemout-is-a-static-field-and-a-null-one-stops-the-title"></a>

#### `System.out` is a static field, and a null one stops the title

A title's own log line is not optional. The module compiles
`System.out.println(x)` into a null test, a throw and a vtable dispatch, so a null
`System.out` does not skip the print: it raises the title's own
`NullPointerException` inside whatever routine was logging. One local title
reaches its first `println` from its Jlet's constructor and stopped there, having
just built the string `INFO! (...)`.

Serving that call needed three things:

- **A static field is a position in its class's storage, not a table index.** The
  module compiles a read as `classObject.data[javaClassDataWords + answer]`, where
  `answer` is the int16 this platform writes into the load call's static-field out
  array. It used to be the entry's global index, right only while the module
  declares one static field and past the end of the block once it declares two.
- **A platform class's class object must be long enough to hold them.** The block
  was sized from the module's own record, which a platform class lacks, so the
  guest's read of `System.out` landed past the arena allocation. It is now sized
  from the load call's static-field run, the same count the slot above uses.
- **`java/io/PrintStream`'s slots follow declaration order**, confirmed by the
  call site: `OutputStream` declares five methods at 10 to 14; `PrintStream`
  overrides four and adds `checkError`, `setError`, nine `print` and ten `println`
  forms in that order, putting `println(String)` at 34, the slot the title
  dispatches with a String in its second register. **`setError` makes it 34, not
  33**: protected rather than public, it still takes a slot.

The line goes where the guest's other self-reporting goes: the debug log, beside
`printk`. A browser page has no console to print into.

<a id="implementation-a-word-that-looks-like-a-pixel-operation-and-is-not-one"></a>

#### A word that looks like a pixel operation and is not one

One title stops inside what this platform ran as its pixel operation, faulting on
a read of `0x98`, a small offset from a null base. `readContextPixelOp` already
refuses a field that is not a Thumb address inside the module, and this word
passes: `0xf81f` is real code in the title's image.

**It is not a pixel operation.** Disassembled, `0xf81e` is the middle of a large
routine that uses `r7` as a structure base, indexes it with byte offsets, and
calls out through a literal pool; a pixel operation is three or four instructions
over three registers. The context's operation word holds whatever the title left
there, and the inside-the-module test cannot distinguish that from an operation,
because a leftover return address is also inside the module.

Knowing whether the title ever installed one would distinguish them:
`MC_grpSetContext` names the field it sets, so a context never given field 5 has
no operation whatever its word holds. That is not done yet: the context is the
game's own structure, read fresh on every call, so no "was it set" record could
exist that a title building the structure itself would not defeat. The title
stays on the list with the probe's findings recorded.

<a id="implementation-a-strlen-is-not-a-name"></a>

#### A `strlen` is not a name

One title read an eight-kilobyte text resource into a `calloc` of exactly its
size and called `strlen` on it. The file contains no zero byte, so a handset
answers the distance to the first zero *after* the buffer, in the heap past the
allocation, and the title proceeds from there.

This platform refused it, because `strlen` read the guest string with a name's
bound: 4096 bytes, past which a pointer is not a name. A name and a buffer are
different questions. `strlen` now measures with a one-megabyte bound and builds no
string, scanning past the allocation as C does and stopping at the zero the arena
left there.

**C does not read a block at a time, as the acceptance probe showed.** Measuring
in 256-byte blocks sped up the common case and broke the most common argument: a
name built on the title's own stack, in the last page of the stack mapping with a
terminator a few bytes in. A whole-block read there runs off the mapping, so the
walk refused a string it should have measured in four bytes. It now falls back to
one byte at a time once a block does not read, so only an unmapped byte marks a
pointer that was never a string. Every local archive booted before and after the
change except under the block read, which `TestLocalLGTArchivesBootAndPaint`
named.

<a id="implementation-what-the-fourth-round-moved-measured-against-itself"></a>

#### What the fourth round moved, measured against itself

The whole 94-archive set was run on the binaries before and after this round, from
an empty save tree, three thousand ticks each, six at a time under a
three-hundred-second wall clock.

**Four titles moved from an error to a finished run, and none moved the other
way.** The other 87 rows are identical in flush, lit-pixel and tick counts. One
title's flush count differs between sweeps as its own noise: alone on the
*unchanged* binary, four runs give 941, 942, 943 and 943, the spread the third
round recorded.

Seven remain: two guest memory faults (the module entry following a word this
platform does not fill, and the operation word that is not an operation), the
interface function `0x64`, the title needing a `DataInputStream` over its own
stream, one exceeding the instruction limit, one unable to finish three thousand
ticks, and one finishing every tick and painting black. Two more hit the sweep's
wall clock and are not failures: alone, one finishes in 223 seconds and the other
within the limit.

The same A/B ran over the other platform's 262-archive set, since the picture
decoder is shared: two titles moved from an error to a first frame, and **none of
the 262 rows changed flush, lit-pixel or tick count**.

<a id="implementation-what-interface-function-0x64-is-read-off-its-call-site"></a>

#### What interface function `0x64` is, read off its call site

One title stops at tick 5 on AOT interface function `0x64`, and the report's
register dump misleads in the way warned about above: `argument 3`, `0x1403278`,
is not an argument but the stub the call site loaded from its literal pool and
branched to. The call takes the receiver and one more reference.

The surrounding instructions settle what it answers:

```
    ldr  r3, [pc, #0x48]      ; a module routine
    mov  lr, pc
    bx   r3                   ; -> r0
    ldr  r3, [r0, #8]
    mov  r0, r4               ; the receiver, an application object
    ldr  r1, [r3, #8]
    ldr  r3, [pc, #0x34]      ; the 0x64 stub
    mov  lr, pc
    bx   r3                   ; -> r0
    ldr  r3, [r0, #4]         ; <- the answer's second word
    cmp  r3, #0
    movne r0, r4              ; present: the receiver
    bne  call
    mov  r0, r3               ; absent: null
    ldr  r3, [pc, #0xa0]      ;         and a fixed routine instead
call:
    mov  lr, pc
    bx   r3
```

**The word at `+4` of the answer is a function pointer, called with the
receiver.** The earlier reading ("it chooses between the original object and
null") missed that the choice is between two *functions*, the object being only
the argument. `0x64` is a **method lookup**, not the type check its shape
suggested, and the fallback is what a failed lookup runs.

**It looks the method up by interface name, as one report line showed.** The
second argument comes from a class object: the preceding module routine is the
accessor checking the initialised halfword at `+0x10`, and word 2 of a class
object's data is its name. The report could not show that while it named only
arguments recognised as objects; it now follows other words as text and then as
the words they point at, and the same call reads `argument 1 is the text
"IEventHandler"`. That is `invokeinterface`: a receiver and the interface the
call goes through.

**The answer is where that interface's methods start in the receiver's dispatch
table.** A class record's interface table pairs each interface with a number, and
the call site reads `+4` of the answer for the interface's first method, so the
answer is the table biased so `+4 + 4n` lands on method `n`: a virtual call's
`vtable + 4 + 4n`, counted from the interface's first method instead of the
class's.

**The entry's second word takes two forms, told apart by value.** For
`java/lang/Runnable` it is a slot, 10, a position in the class's own vtable, as
the thread path reads it. For the interface this title dispatches through it is
`0x87f80`, an address in the module's text, followed by five more: the entry *is*
the interface's method table. A slot is smaller than the class's vtable and an
address is not, so the number decides, and each is answered as its call site
expects.

<a id="implementation-a-stream-a-title-wrote-itself"></a>

#### A stream a title wrote itself

`DataInputStream` and `InputStreamReader` are declared over the abstract
`java/io/InputStream`, not over a resource this platform opened, and one of the 94
titles hands the first a stream of its own. The wrapper stood for the same open
resource as the underlying stream (correctly, so a read through either moves one
cursor) and had nowhere to go when the underlying object was compiled by the
title. It refused, and the title stopped in its `<init>`.

Such a stream is read as the title's own callers would: a virtual call into its
`read`, a block at a time, into the buffer a resource stream fills. Every reader
above (`readInt`, `readUTF`, `readFully`, the reader's character decode) requests
the bytes it needs before looking, which is free for a stream this platform holds
whole, so one code path serves both. The window holds only unread bytes: nothing
seeks backwards, so bytes behind the cursor are dropped on each pull and a long
stream costs a block, not its length.

**The methods are in the object's vtable, not its class record**, the whole
difficulty. A class the compiler laid out declares no member records (its
dispatch table is in the image), so looking `read` up by name and descriptor
finds nothing on exactly these classes. The slots are `java/io/InputStream`'s,
which this platform numbers, and an override sits at the inherited number: 10 for
`read()`, 12 for `read([BII)`, 14 for `available`. A slot is an override when the
subclass's entry differs from the platform class's; an equal entry leads straight
back here.

`read([BII)` is preferred and `read()` is the fallback, because the class library
defines the first via the second and a subclass must override at least the
second; a class overriding neither is not a stream and is refused by name. Each
byte is a guest call, so the fallback is asked for exactly what the reader wants,
not a block. `available` is the one question the window cannot answer (what was
pulled says nothing about what remains), so the title's own `available` is called
when overridden, and otherwise the window answers, the specification's floor.

A stream whose `read` reads back through its own wrapper would recurse until the
host stack ended, so a pull in flight is refused rather than re-entered, and the
refusal names the class.

**Three more walls followed, the first the worst kind.** The title stopped
failing and then stopped *entirely*: three minutes of wall clock for four
hundredths of a second of CPU, no error, no last frame. A goroutine dump named it
in one pass (the session goroutine blocked sending a slice to a thread whose
goroutine had ended); see "A thread that has ended is never granted another
slice" below. The other two were ordinary: `new ByteArrayInputStream(null)`
reported a platform failure where the language throws, stopping a title that
catches and logs its own nulls a few lines earlier in the same run; and
`Graphics.getTranslateX` and `getTranslateY` were unimplemented although their
state was always kept. With those the title paints its own notice screen.

**Closing the wrapper does not close the title's stream.** Both objects stand for
one open stream, so a close marks it closed and later reads say so, but the
title's own `close` is not entered: nothing has needed it, and an invented close
is guest code running at a moment the title did not choose. A title closing both,
the language's idiom, closes its own either way.

<a id="implementation-a-thread-that-has-ended-is-never-granted-another-slice"></a>

#### A thread that has ended is never granted another slice

The session hands each guest thread a slice and waits it out:
`worker.grant <- ctx`, then `<-worker.events`, against a worker goroutine that
receives the grant, runs, and sends back a park or its end. **A grant to a worker
whose goroutine has returned blocks for ever**, taking the session with it: no
error, no last frame, no CPU, only a sleeping process.

The end was recorded by the three *callers*: the tick's `ServiceJavaThreads`, the
platform thread's `runOtherJavaWorkers`, and a monitor wait. The monitor wait got
it wrong structurally: it grants a slice to **the lock-holding thread**, not the
thread it was called about, so the end it receives belongs to a worker it does not
track. It checked the error and dropped the event. One title's guest thread threw
out of `run` while holding a lock, ended inside exactly that grant, and was
granted again the next tick.

The end is now recorded where the slice is granted, and a worker known to be
finished is answered rather than sent to, so all three callers (and any fourth)
are right by construction.

**A hang is the worst failure this package can produce**: every other failure
names a slot, address or class and the sweep's judgement catches it, while this
one produced a "timed out" row that reads as a slow title. The title had stopped.

<a id="implementation-a-frame-drawn-into-a-picture-wiped-on-its-way-to-the-screen"></a>

#### A frame drawn into a picture, wiped on its way to the screen

One title finished three thousand ticks with no error and no lit pixel while
drawing throughout: 334 drawing calls a frame, then one `drawImage` onto the
screen. It double-buffers: it creates a 240x320 picture, keeps its `Graphics` in a
field, paints the frame into it, and blits it to the screen in `paint`.

**The blit re-read the picture from guest memory first.** Every source surface is
re-read before use because a Clet writes pixels through its framebuffer pointer
(see [A Java title's drawing does not synchronise, and a Clet's has to](#implementation-a-java-titles-drawing-does-not-synchronise-and-a-clets-has-to)).
A Java title never receives a surface address or writes pixels through guest
memory, so for the picture it had just painted the re-read was a wipe with the
guest's stale zeros, once per frame, on the way to the screen.

A surface an AOT `Graphics` has drawn into is now marked, and a marked surface is
not read back. The mark is set where the drawing context is built, the one place
every Java drawing call passes.

**The judgement passed it.** An LGT row counts as working when it finishes its
ticks, and this one did; a black screen shows only in a frame. `framestats` makes
that visible without a person; see [`cli.md`](../cli.md).

<a id="implementation-a-flush-names-the-surface-to-show-and-the-screen-is-not-always-it"></a>

#### A flush names the surface to show, and the screen is not always it

`MC_grpFlushLcd(lcd, frm, x, y, w, h)` takes the framebuffer to display as its
**second** argument, which this platform ignored, publishing the LCD's surface on
every flush. That suits titles drawing on the LCD directly (most of them) and
fails the one double-buffering in C: it asks for the screen framebuffer, never
draws on it, creates its own surface, draws its whole frame there and flushes
*that*. Nothing it drew reached the panel.

A flush now copies the named surface onto the display when it is not the display,
and reads the display back when it is. A Clet needs the read-back because it
wrote its pixels through its pointer rather than through any call this platform
saw.

<a id="implementation-what-a-host-shows-is-what-the-last-flush-put-on-the-panel"></a>

#### What a Host shows is what the last flush put on the panel

The frame a Host takes used to be the framebuffer converted when it asked. On a
handset those differ: `MC_grpFlushLcd` copies a surface to the panel, and later
drawing into that surface stays invisible until the next flush. The difference
can be a whole title.

One title fills the screen white, flushes, and immediately fills it black to start
the next frame. Every flush left the screen white and every frame a Host took was
black, for three thousand ticks, because the guest kept drawing between the flush
and the ask. It looked like a title that never draws, and was one whose every
frame was discarded.

The display is now a copy taken at each flush (both C flush slots and the Java
paint), and `Frame` and `FrameDigest` read that copy. **This changes every title's
frames**, so it was measured over the whole 94-archive set before and after.

<a id="implementation-a-width-that-ignored-the-count-it-was-given"></a>

#### A width that ignored the count it was given

`MC_grpGetStringWidth(font, str, len)` measured the whole string whatever `len`
said. That suits every caller passing `-1` (the specification's "or -1 for
null-terminated") and fails the one that matters: a title wrapping text grows a
run one character at a time, asking its width until it no longer fits. Answered
the same width every time, the loop cannot end.

That title had been listed as "instruction limit exceeded", which reads as too
much work. **The trace's tail told them apart**: a loading title crosses a mix of
boundaries, while this one's last forty crossings were the same call with the
same arguments and answer. It was stuck, not slow, the distinction the hang above
turned on, found by the same tool.

<a id="implementation-reclaiming-what-a-java-title-stopped-holding"></a>

### Reclaiming what a Java title stopped holding

Until this point nothing on this path reclaimed anything. `java_stream.go` said so
(*"nothing here ever reclaims, because the Java path has no collector"*), as did
the note beside the decoded-image cache in `java_runtime.go`. A session held every
object a title ever allocated, and one title reloading its sprite sheets inside
`paint` filled the surface region and ended its run.

`collect.go` is the collector. Its design is `internal/platform/ktf/collect.go`'s,
already carrying titles on the other platform, and the reasoning transfers: the
guest side is conservative (every word of registers, stacks and committed
read-write memory is a candidate pointer, because over-retention is safe and
freeing a live object corrupts the guest), and an object is never freed in the
cycle that first finds it unreachable.

Two aspects are this platform's own.

**An object is two allocations.** The three-word object and its field block are
separate arena blocks, and a field access loads the block's address first, so the
guest may hold either. Both spans are indexed to the same object, and freeing one
frees the other.

**An object's references are not all in its payload.** What a stream, image,
file, vector, widget or wrapper stands for lives in `javaRuntime`'s maps, keyed by
the holding object, with three kinds of edge no guest word expresses:

1. **the surface** an Image or Graphics draws into, in a different arena, which
   must be returned with its holder;
2. **the open file** a `File` object stands for, which must be *closed* (buffered
   writes flushed to the store) rather than forgotten, as the language would do on
   a handset;
3. **aliases**, two guest objects naming one native thing: two Images over one
   decoded surface, a `DataOutputStream` over another object's sink, a stream
   bound to the `File` it was opened on. A native payload is returned only when
   nothing live names it, which for a surface includes the decode cache.

`walkJavaPayload` is the reference-carrying half and `releaseJavaPayloads` the
owning half. Missing either frees a live object, since nothing in guest memory
names it.

The platform's own handles are roots, not edges: the application object, the
displayed card, the Graphics a paint is handed, singletons a title asks for by
name, work `callSerially` holds, a started thread and its runnable, and an object
whose monitor is held. No guest word names these between frames.

The surface region is not scanned for roots. A Java title never receives a
surface address or writes pixels through guest memory (the fact
`framebuffer.drawnHere` exists for), so the region holds only pixels, and walking
sixteen megabytes of them would buy only false retention.

<a id="implementation-two-triggers-and-why-the-second-one-is-needed-twice-over"></a>

#### Two triggers, and why the second one is needed twice over

The first is arena growth, a fixed 256 KiB ahead of what survived the last cycle.
Growing the trigger with the arena instead lets the arena keep climbing, because
an object is freed only a cycle after it becomes unreachable.

The second is **a refused allocation**. A title reaching the end of a region
between cycles is told the arena is full while the objects that would make room
are already unreachable and waiting for the next round, so `client.allocate` and
`client.allocateSurface` run a cycle and ask once more. That cycle runs inside a
platform call rather than at the end of a service round, made safe by the grace
cycle (nothing allocated since the last cycle can be freed) and a **call-scoped
pin set**: every object a platform call allocates is a root until the call
returns, because a half-finished call holds objects in Go locals the scan cannot
see. Pins are taken and released at the supervisor-call boundary, so they nest
with the calls and are empty at the end of a round. The same trigger is now on the
KTF path, where the Go-side weak binding already answers what the pins answer
here.

A third condition is less a trigger than the grace cycle completing. A condemned
object's bytes are still outstanding, so a title that fills the arena with garbage
and stops allocating never grows the arena again and never reaches the freeing
cycle: on one title the first cycle condemned 980 of 2168 objects and the arena
then sat still for the rest of the run, which 296 KiB of growth alone would never
return. A cycle that condemned anything therefore arms another eight service
rounds later. The interval prevents a cycle every tick: a steadily allocating
title always has something condemned, and returning every round cost one title
about a millisecond a tick, against 0.13 ms at an interval of eight for the same
bytes reclaimed.

<a id="implementation-what-it-reclaims-and-the-frames-that-say-it-changed-nothing"></a>

#### What it reclaims, and the frames that say it changed nothing

Four of the 35 local archives are Java titles; the rest are Clets, where no
collector runs. Each was run twice from one build, collector off and on, comparing
the presented frame at every tick. Over 3000 ticks:

| title | tracked | freed | bytes | arena, off → on | collector cost |
|---|---|---|---|---|---|
| A | 2168 | 980 | 296,461 | 571,808 → 270,784 | 5.6 ms |
| B | 472 | 59 | 10,148 | 82,824 → 72,336 | 3.8 ms |
| C | 228 | 8963 | 289,428 | 410,688 → 49,608 | 393 ms |

The frames are identical tick for tick in every one, which is the judgement that
matters: a byte count does not show a title still draws the same thing, and
`collect_probe_test.go` asks the question the right way round.

No surface or open file was reclaimed in those runs (no local Java title drops an
`Image` or a `File` within 3000 ticks), so those three payload edges are covered
by `collect_test.go`, not the corpus.

<a id="implementation-deliberately-incomplete"></a>

### Deliberately incomplete

- **LGT Java apps play, and the platform API behind them is partial.** The class
  protocol, arrays, exceptions, monitors, threads, text, resource streams and the
  drawing surface are implemented. Whatever no title has asked for is not: a
  missing method stops the title with its own name rather than a wrong answer,
  which makes the next one cheap to add. See "What the running titles say now".

  Status of each:

  - three reach a play screen and are driven from the keypad. **A route reaching
    a play screen does not cover the menus**: the menu branch that stopped at
    `Vector` slot 19 was found by a denser key script than the playing one, and
    the four Vector slots and the type-check argument behind it are now served;
  - one stops on its own save-backup check, below;
  - one title's data-sending menu branch reaches the title's offline dialog, a
    missing server rather than a gap (see [A class with no member list is entered
    through its interface table](#implementation-a-class-with-no-member-list-is-entered-through-its-interface-table);
    it also took the handset property and `String.equals`).

- **A save-backup check a refused connection leaves up for ever.** The fourth
  Java title paints "checking the save backup" once and never repaints. No
  missing API is reported, and it is not the save file: the file opened read-only
  just before is absent, the `IOException` is *caught*, and a second run on the
  tree the first wrote stops at the same place. **Every key was swept and every
  run ends on the same frame**, so it is not waiting for a key. It is the title's
  own online gate, readable from four methods of one class:

  - the card class every screen extends declares `paint`, inherited by every
    screen. It draws a notice while a static word is set; when the word is zero it
    calls a private helper whose last instruction clears an instance flag;
  - `while (flag != 0) Thread.sleep(10)` opens the method the platform's frame
    loop calls to run a screen, so a screen starts only after one paint **without
    a notice up**;
  - the save-backup routine raises the notice, constructs
    `org/kwis/msf/io/Network` and calls `connect`. On the refused connection's -1
    it branches straight to its epilogue with result 2, **skipping the call that
    takes the notice down**;
  - the menu passes that 2 to the method pushing the next screen. The notice is
    still up, so that screen's loop waits for a paint that cannot happen.

  The title deadlocks itself when the network is refused, with no key, timeout or
  second exit. **A false yes does not rescue it**: answering `connect` with 0 (the
  specification's "access is available") stops it nine ticks in at the
  unimplemented `org/kwis/msf/io/URL.find`. The refusal gets the title furthest.

  Two earlier readings were wrong: the flag-clearing paint belongs to the
  **superclass of every card in the title**, not a sibling of the displayed card,
  and the clearing store is the tail of `b(Lorg/kwis/msp/lcdui/Graphics;)V`, the
  method **before** `a(Lorg/kwis/msp/lcdui/Graphics;)V`. Both came from reading a
  member list without the class hierarchy; the backtrace under "Where a title is
  waiting" below settled it in one run, and "The gate, and what holds it open"
  has the full disassembly.
- **A title's own `print` goes to the log only.** `System.out` and `System.err`
  are one `PrintStream`; every `print` and `println` renders its argument as one
  debug line beside `printk`, where the C side's self-reporting goes. A browser
  page has no console and a print has no file to reach, and a stream answering
  `write` into unread bytes would be the same with more machinery. A release
  build counts it like every other debug line.

- **Only the two platform statics above are filled in.** A platform-owned static
  field is storage the module reads directly from a class object's data block,
  sized from the run the load call numbers, so an unknown field reads zero rather
  than faulting. That is right for a reference (`null` is testable) and wrong for
  a number, and no local title asks for one.

- **WIPI-C text entry is append-only from the Host.** The input-method boundary
  exposes completed and composing buffers but no stable component, current value
  or cursor, so **Opts → 문자 입력** can insert complete text at the guest cursor
  after the widget has used `MC_imHandleInput` but cannot display or replace text
  the widget holds. See [Character input](#implementation-character-input).
- **A scene load takes seconds, with the loading art held throughout.** Measured
  after the page-permission change in `docs/armcore.md`, on an M-series desktop,
  release profile: the worst single tick across the local archives is **6.4 s**
  (best of five; repeats land between 6.4 and 7.1 s, and the debug profile
  measures the same, as an all-guest-ARM cost should). It is a chapter-based
  title entering its first scene. RPG world entries are smaller: 2.6 s for the
  heaviest, 1.2 s for the next, and most titles never exceed half a second in a
  tick.

  To measure: `runlgt -ticks N -key ...` with a key script reaching the load, then
  `slowest_tick_ms` from the JSON summary (an average hides a load inside one
  timer callback); take a best of several, since it moves by a tenth between runs
  on a warm machine. The budget is not the limit (roughly 6e8 instructions against
  a 3e9 ceiling), so this is interpreter throughput; `docs/armcore.md` has the
  measurements and abandoned attempts. The earlier "about ten seconds" predates
  the page-permission change and timed a world load rather than a title and
  scene, so the difference cannot be apportioned between the change and the load.
- **A draw still copies the rows it touches, twice.** The runtime keeps its own
  copy of every surface, so a draw reads the reachable band from guest memory
  before drawing and writes it back after, because a Clet may have written and
  will read those pixels through its pointer. The band made this cheap (above);
  a draw on the guest's own bytes would make it free, and nothing local waits on
  that.
- **No external network.** Authentication compatibility can select the bounded
  [local notification and empty-save responder](../authentication.md#lgt-local-notification-and-empty-remote-save-service)
  or [binary authentication responder](../authentication.md#lgt-local-binary-authentication).
  Otherwise the block reports failure, which a game's state machine handles,
  whereas a claimed connection would wait for data that never arrives. The
  *whole* block refuses, not just connect, because a refused title still tears
  down what it started, and stopping it at the teardown turns a handled refusal
  into a crash. `MC_netClose` returns void, so it reports success.

  `MC_netConnect` reports failure through its callback rather than its return
  value, because the specification answers an accepted dial that way and never
  calls back a refused one; answering only the return value left a title waiting
  forever. See `wipic_net.go` and `docs/network.md`.

  An online-gated title without an offline path still cannot be played (a missing
  server, not a wrong stub), but its response is now its own decision: one asks
  whether to authenticate and takes "no", another reports the failure and
  continues to its title screen, and a third shows its anti-piracy notice and
  exits from its own callback. **That exit is the game's**: its last three platform
  calls resolve import table `0x1fb` entry `0x68` and call it, `MC_knlExit` from
  the title's code. A title resolving the exit slot lazily when it wants to quit
  has decided to quit, and answering the connection differently would be false.
  Take `-trace N` and the last line before filing an online-gated title as a
  defect.
- **Animated images.** `MC_grpDecodeNextImage` reports an image whole as soon as
  it is created, because nothing here decodes an animated encoding and there is
  no frame clock to run one against, as on the other WIPI platform.
- **The tests do not load a real archive.** With no LGT archive in the repository,
  they use a newly authored ARM module (`fixture_test.go`) that resolves its
  imports, registers a Clet, writes the framebuffer directly and flushes, plus
  slot-level tests; that pins known contracts but cannot *find* one (see
  [Testing](#implementation-testing)).
- **The restart notice is dismissed by the key it offers, and the run it ends is
  its own.** One title draws "the game downloaded correctly, restart it to secure
  memory". A key sweep ended every run on the same frame, read as "no key
  dismisses it", wrongly: **the title answers the key by saving and exiting**, so
  the last frame is the notice either way. A final-frame sweep cannot see a title
  that quits.

  From the module and a live run: the notice is screen 23 of the title's screen
  table, chosen when a word in its state block is zero. On OK the handler sets
  that word, writes a hundred bytes to `fs/nv.dat` and calls `MC_knlExit`, as the
  notice asks. **A second run on the same tree goes straight past it**, through the
  usage notice, mode select, publisher screen and loading bar, to its "the
  connection is delayed" dialog and then its character naming screen, where WIPI C
  slot `0x321` was unimplemented. The notice is a first-run step.

  **`Zio_Confirm` is a red herring**: the eight-byte record is read as earlier
  stated and the notice does not depend on it. The run after the notice found a
  real core gap: an ARMv6 `nop` in a Thumb-1 module, the mark of a **patched
  archive** where a cracker wrote two bytes over a branch (`docs/armcore.md`, "The
  hint space"). Reading the module alone had been wrong twice; `-trace-live ''`
  (showing `fsOpen("nv.dat", read-write)`, a write, then the exit after the key)
  and the cheat console's `read` (which screen the title believed it was on)
  settled it.

  It entered the library under the wrong vendor (`app_info` inside, in
  `var/games/ktf`), where the KTF acceptance test walks the directory rather than
  sniffing each archive, so it failed as "no `__adf__`" and revealed nothing;
  `detect.Archive` had it right. A test that walks a vendor directory trusts the
  directory.

  **A second title's notice of the same family hides its answer elsewhere.** Its
  dialog reads "please run it again for a stable game" over a button labelled
  `OZ`, and a final-frame sweep again showed nothing. The key is **`fire`** (the
  label is button artwork), and it writes one byte of the title's `fs/p.dat`, from
  `…01 00` to `…01 01`, **leaving the notice up for the rest of the run**; the
  notice refers to the next session. A second run goes past, through two consent
  screens, the usage notice, the type select and its confirm, and the opening
  narration, which `#` skips into the opening scene (a tavern, an NPC with a quest
  mark, an HP/MP bar over a hotkey row). **Sweep the save tree beside the frame**:
  twenty-three keys, one file, one byte, and the answer is in the diff. Both runs
  are that title's first-run and in-game routes under `var/routes/`.

  **`OZ` on a button is artwork, as a third title confirms**: it asks for the OZ
  key in words and also takes `fire`. Neither title asks for `soft3`, which matters
  because the page carries one soft key (`cli.md`, "Key names"; the keypad's
  `Menu` button is `soft1`), so a title truly needing the other two would be
  unplayable in the browser.

  **The first title then plays its opening.** With slot `0x321` implemented and
  the component block refused (see [Block eight is the component block](#implementation-block-eight-is-the-component-block-and-0x320-is-not-a-clock)),
  the run reaches the usage notice, consent screen, delayed-connection dialog,
  title, slot select, and a name prompt. In its own naming field **the OK key
  cycles preset names rather than confirming** (빈스, 아론, 루크, 에반, 세라,
  숙자), so a script pressing it once reads as a screen ignoring its prompt;
  pressing past the end commits. The opening's narration honours `SKIP:#`: a route
  alternating `#` and `fire` sixty ticks apart reaches the dungeon in about four
  thousand ticks (party sprites, enemies, an HP/MP/EXP bar and a `콤보 타격 0/3`
  counter). `fire` attacks and a **held** direction moves (`press down` /
  `release down` scrolls into the next room, where the HUD moves aside). Nothing
  the party's progress depends on (save, shops, menus) has been driven yet; a route
  there starts from this script.

<a id="implementation-a-transparent-pixel-still-has-a-colour"></a>

### A transparent pixel still has a colour

A guest surface is RGB565 with no alpha channel, so an encoding's declared
transparency must travel elsewhere, and this platform sends it two ways because
two readers need it.

`framebufferFromImage` keeps an `opaque` mask beside the pixels, consulted by
every draw path the platform owns. **The other reader is the game.** One local
title never asks the platform to blit a sprite: over a run reaching its
character-select screen it makes 178 `MC_grpCreateImage` calls, 178
`MC_grpGetImageFrameBuffer` calls, **51,170** calls for a framebuffer's raw
pointer, and no `MC_grpDrawImage`. It decodes through the platform, takes the
pointer, and runs its own blitter over the words; the platform contributes only
the pixels.

For that reader the colour *is* the transparency. Every one of the 52 paletted
PNGs this title ships with a `tRNS` entry declares the same colour: **pure
magenta, `#ff00ff`**, `0xf81f` in RGB565, already the platform blit's mask colour.
The blitter skips matching pixels.

Go returns colours premultiplied, so a zero-alpha pixel comes back from `RGBA()`
as **black, whatever the artist put there**, and writing that into the surface
deletes the only transparency the guest can see. The blitter's key then matches
nothing and it paints the sprite's whole rectangle: a character on a solid box, a
logo in a grey slab, a battle effect in a black square, wherever a title blits
for itself.

The conversion therefore reads the **non-premultiplied** form,
`color.NRGBAModel`, and records the mask as before. `DecodeBitmap` had cleared
its declared entry to a zero `color.NRGBA` for the same reason and now clears
only the alpha.

How it was found:

- **The symptom was invisible at 1x.** In a captured 240x320 frame the boxes did
  not register and the screen was called clean; through the hqx filter they
  showed at once. Magnify a capture that disagrees with a player's screenshot
  before disbelieving the screenshot.
- **The call histogram answered "whose bug is it" in one run.**
  `runlgt -trace-live ""` over a route to the screen, with slots counted, showed
  the platform was never asked to draw a sprite, ruling out every blit path and
  leaving one thing the platform hands over.

<a id="implementation-an-lgt-ab-is-exact-but-only-when-the-runs-are-alone"></a>

#### An LGT A/B is exact, but only when the runs are alone

Driving all twenty-eight local archives through one generic route under two
builds and comparing captured frames is the regression net here, and unlike the
KTF sweep it has **no noise floor**: a route replayed on the same binary produces
a byte-identical frame, because the guest clock is virtual and a route counts
ticks, not seconds.

That holds only for a run with the machine to itself. The sweep above ran six
archives at a time, and one title differed between builds; re-run alone, **the
same binary disagreed with its own sweep capture** while the two builds agreed,
so the load caused the difference, not the change.

A difference found in a parallel sweep is therefore a candidate, not a finding:
re-run that archive alone under both builds first. Of eighty-four pairs, three
differed and one was this.

<a id="implementation-two-titles-two-opposite-ways-of-drawing"></a>

#### Two titles, two opposite ways of drawing

The fix above does nothing for the title reporting a similar symptom, as platform
call counts show. Over one route each:

| | title that blits for itself | title that lets the platform draw |
|---|---|---|
| `MC_grpDrawImage` | **0** | **188,587** |
| `MC_grpFillRect` | 0 | 239,871 |
| `MC_grpSetContext` | 6,644 | 498,685 |
| framebuffer pointer | 51,170 | 87,332 |

The second title's transparency already works, though not through its
declaration: of its 2,282 BMPs, 2,244 declare a transparent entry at index **256**
against a 256-entry palette, out of range and so ignored, as in the status-bar
case above. That does not matter, because 63% of those bitmaps are dominated by
`#ff00ff` and `drawImage`'s fallback keys on magenta anyway; two independent
routes to the same answer kept the symptom away.

What remained for that title was the unread part of the context.
`grpContextAlpha` and `grpContextPixelOp` were carried in the guest's structure
and handed back on request, but **no draw path consulted either**, so anything
the title meant to blend arrived opaque. It sets field 5 85,830 times in one
route with values like `0x2a75` and `0x23bd`, read at the time as colours
suggesting an off-by-one field numbering with 5 as `param1`. Superseded: they
are Thumb addresses; see [A pixel operation is a function the game installs, not
a colour](#implementation-a-pixel-operation-is-a-function-the-game-installs-not-a-colour).

<a id="implementation-an-operation-this-platform-was-never-handed-is-not-an-operation"></a>

#### An operation this platform was never handed is not an operation

The pixel-operation word had one test, a Thumb address inside the loaded module,
which **a return address passes**: one title's context carried `0xf81f`, the
middle of one of its own routines (see [A word that looks like a pixel operation
and is not one](#implementation-a-word-that-looks-like-a-pixel-operation-and-is-not-one)).
Run as an operation it walked an unset structure base and faulted on a read of
`0x98`, two ticks in, before the title had drawn anything.

The second test is whether this platform was ever *handed* the address:
`MC_grpSetContext` records every operation arriving through it, and a word not
among them is a leftover. The set is per client rather than per context
deliberately: a title that copies a context, saves and restores one, or builds a
second carries the operation's address along, and only an address never
installed anywhere is refused. `MC_grpInitContext` zeroes the record, so a
context never given an operation has none, which makes the absence meaningful.
The title now runs three thousand ticks and holds its own notice screen.

<a id="implementation-where-a-titles-own-dialog-is-the-whole-answer"></a>

#### Where a title's own dialog is the whole answer

Three of the seven titles holding a screen were not held by this platform, and
telling which took a key and a second run rather than a fix.

- **One was waiting for a key.** Its screen is the carrier's "this game costs
  nothing extra" notice ending in "press any key"; pressed, it reaches its main
  menu. Nothing was wrong.
- **One is `MC_fsRename`** (see [Four platform slots the specification's own
  ordering named](#implementation-four-platform-slots-the-specifications-own-ordering-named)).
  With the rename served it writes its save, and a second run (as its screen asks)
  reaches the title's own dialogs.
- **One holds its authentication screen, and a successful dial moves it.**
  Refused, it prints `Connect CB Error [-1]` and sits at its 인증중
  ("authenticating") screen indefinitely. Given a *successful* connect with
  everything else unchanged (every later socket call still refused), it asks its
  own question instead: there is no certificate, shall it connect once to fetch
  one at the player's expense? Answering no reaches the title screen. **The
  refusal policy is unchanged** (docs/network.md explains why connections are
  refused); this records that for this title the refusal is the wall, with an
  offline path behind a question it never gets to ask. **A second title with the
  same question is a different case**: it asks without being dialled first, and
  its own 예 dials, is refused, and reaches its main menu behind the resulting
  error dialog; the wall there was the button its dialog starts on, not the
  refusal ([`network.md`](../network.md)).

**A title's "the save failed" dialog is not always a save.** One shows "data could
not be saved, the game is closing" over an empty save tree, which reads as a write
that never happened, but the trace shows no write was attempted: across a full
run the title calls no filesystem, database or free-space slot. It decided
without asking this platform anything, so the write path is not where the answer
lies.

<a id="implementation-what-the-sweep-costs-now-per-title-and-what-the-profile-says-about-it"></a>

#### What the sweep costs now, per title, and what the profile says about it

The titles far slower than the handset were re-measured on the current binary
over the sweep's three thousand ticks rather than the older figure's four hundred.
**Everything now finishes**: the title that ran for 420 seconds and then 27
minutes without reaching three thousand ticks now reaches them.

Ranked by host busy time, each run alone with a fresh save, the top of the set:

| host busy | guest instructions | ns/instruction | guest time |
|---|---|---|---|
| 207.6s | 8.98G | 23.1 | 150.0s |
| 128.9s | 5.92G | 21.8 | 75.1s |
| 96.7s | 13.04G | 7.4 | 116.1s |
| 90.2s | 11.19G | 8.1 | 73.1s |
| 81.4s | 10.65G | 7.6 | 104.4s |

Guest time is what the title thinks passed, so a title keeping up with the
handset takes that much wall time: three of the five run *faster* than the
handset, and the first two are slower, by 1.4x and 1.7x. Host cost per guest
instruction separates them (23 and 22 nanoseconds against 7 to 8); three of the
five retire more instructions at lower cost, so **cost per guest instruction, not
the count, matters.**

**For the slowest, a whole-run call trace names one slot.** `-trace-live ""` over
forty ticks records 3,171,517 platform calls, 3,096,685 of them
`MC_knlCurrentTime`: seventy-seven thousand clock reads per tick. The guest
profile agrees: 57.7% of instructions in one 180-byte loop and 19.7% in the
platform stubs it calls, the hottest stacks naming that slot's stub. It is a
busy-wait, expensive here because every clock read is a supervisor call out to
the Host and back. The [work clock](#implementation-the-guest-clock-advances-with-work-not-only-with-ticks)
ends the spin (a tick-only clock never would), but only after the guest retires
the instructions the rate charges for, one Host crossing at a time.

**The spin is now served inside the quantum.** The clock is the one slot reading a
number the platform already holds (no guest memory, no callback, nothing that can
block), so it is answered through `armcore.Core.SetFastSupervisorCall` (see
`armcore.md`, "The supervisor-call boundary, and the slots that do not need it"),
which keeps the quantum running. Over three thousand ticks from a fresh save:

| | host busy | guest time | ns a guest instruction |
|---|---|---|---|
| before | 177.5s | 150.0s | 19.8 |
| after | **147.6s** | 150.0s | **16.4** |

The title that was **1.18x slower than the handset now runs at 0.98x**. A second
title polling the slot (13.6M reads in forty ticks, on a run never slow enough for
the table) is 40% cheaper. Across all ninety-four local archives at 400 ticks the
total is −3.6% and **no frame differs**: only the granularity moves, from one
update per call to one per 16,384-instruction quantum, a tenth of a millisecond.
**A trace turns it off**, because a run recording platform calls must record the
dominant one, which is why `-trace` costs this title so much.

**The slowest title's time then went to drawing.** With the clock served, the
profile was 34% `runtime.memmove`, all `syncFromGuest` and `syncToGuest`: every
`MC_grp` call copied the whole destination surface in and out, and of 73,700 draw
calls in forty ticks **72,929 are `MC_grpPutPixel`**, a software rasteriser
working through the platform at two screens of copying (307KB) per pixel.

**A draw now syncs only the rows it can touch**: the context's clip, narrowed by
the slot's own rectangle where it has one. Every such operation draws through the
`put` that refuses pixels outside the clip, so the clip is always a correct band,
even for a slot the switch does not name. `rowBand` in `graphics.go` and
`drawBand` in `wipic_draw.go` use rows, not a rectangle, because a row-major
surface makes a band one contiguous run.

Over three thousand ticks, the two changes together on that title:

| | host busy | guest time | ns a guest instruction |
|---|---|---|---|
| before | 177.5s | 150.0s | 19.8 |
| the clock served | 147.6s | 150.0s | 16.4 |
| and the band | **75.5s** | 150.0s | **8.4** |

It went from **1.18x slower than the handset to 0.50x** (twice the handset's
speed) on the same 8,978,757,686 instructions. Across all ninety-four local
archives at 400 ticks the total is **−10.8%** with **no frame differing**, and ten
titles fall by 14% to 68%: the band helps titles drawing through `MC_grp` rather
than into their own buffer, and does nothing for the others. Three apparent
regressions in the single-shot sweep were within the noise floor best-of-three
(+2.0%, −0.6%, −1.1% on runs of about a second).

**The other four are their own code.** None calls any slot more than a few tens of
thousands of times over forty ticks, their traces are ordinary (`memcpy`,
`strcmp`, framebuffer queries, fills), and each profile has the familiar shape:
67% to 92% of instructions inside one loop of a few hundred bytes.

**The second slow title is a software rasteriser.** Its two hot ranges are 93% of
everything it executes, both pure guest arithmetic with no platform call:

- **71%** is a per-pixel channel transform over an RGB565 surface: split each
  pixel with `lsr #11`, `and #0x7e0` and `and #0x1f`, look each channel up in a
  byte table, recombine with `lsl #11` and `lsl #6`, and store the halfword back,
  about twenty-five instructions per pixel, over a region bounded by two words of
  a structure it carries, once a frame.
- **22%** is a run-length decoder with a 16.16 fixed-point scaler: read a run
  byte, `mla` the step into an accumulator, take the integer part with `asr #16`,
  keep the fraction with the signed `asr #31 / lsr #16 / lsl #16 / rsb` idiom, and
  index a sixteen-bit palette with the next byte: a stretched sprite blit.

Nothing on this side is at fault. It is 1.7x slower than the handset because it
rasterises in its own code, every instruction interpreted; the levers are the ARM
core, or recognising the routine and answering it natively, the shape of KTF's
binary hooks, which LGT lacks. Neither is a change for one title.
[`cli.md`](../cli.md) has the disassemble probe that takes a profile's address
ranges.

<a id="implementation-what-a-module-resolves-and-when"></a>

#### What a module resolves, and when

`internal/tools/apiscan` reads an LGT title's platform surface by running its
initializer and listing what it resolved, assuming a module resolves everything
it might call before running its own code. **That holds for most modules, not
all**: one resolves lazily, asking for `MC_grpSetContext` and `MC_grpDrawImage`
as it first reaches them, interleaved with its own work. A scan reporting no
unimplemented slot reports what the title resolved *by the end of its
initializer*: everything for an eager module, only start-up needs for a lazy one.

Over all ninety-four archives the scan reports no unimplemented WIPI C slot (its
gaps are in the Java and C-library tables), and running the sweep to four hundred
ticks adds none. That is why the database block (base `0x440`) stays
unimplemented: the empty list `MC_dbListDataBases` answers is the whole answer for
every title here, and serving `MC_dbOpenDataBase` would build a storage surface no
caller shapes. It is a measurement to re-run, not a conclusion to keep; a new
archive could change it.

<a id="implementation-a-module-whose-elf-header-names-the-wrong-entry"></a>

#### A module whose ELF header names the wrong entry

The section below treats a module's fault as a question about the block it was
handed. **That was the wrong question**: the module was not entered where it
begins. Its `e_entry` is `0x1000`, where its Jlet registration routine reads a
class name from a block the platform should have filled first, so the fault is
real but a symptom of entering by the wrong door.

**The toolchain records the entry twice, and only here do the two disagree.**
Every module carries a `.raptor` section: magic `RAPT`, a version, its length,
the entry as an offset from the image base with the Thumb bit set, and later the
libraries the module wants (`kernel dlet cldc wipijava lgte`). In 127 of the 128
local modules that word plus the image base equals the ELF header's entry. In the
other it names `0x3baec`, and entering there runs the real start: the module
fills its init struct, its initializer runs, the platform's launcher enters the
application's Jlet, and the title reaches its notice screen and then its opening.

The entry is therefore read from `.raptor` when the section exists and names an
address inside an executable section, and from the ELF header otherwise. The
check matters more than the preference: an unchecked offset would turn a readable
failure into a wild jump.

**The module then asked for four small things**, each stopping it at the next
tick: `Graphics.drawSubstring`, the run-of-a-String companion to `drawChars`;
`Display.getGameAction` and its reverse `getKeyCode`, which a title handling the
pad through actions asks for in `keyNotify`; and the two `BaseClip` methods below.

The action conversion covers WIPI soft keys (`-6`, `-7`, `-8` to `90`, `91`, `92`)
and side keys (`-13`, `-14`, `-15` to `96`, `97`, `98`), in both directions, with
action numbers from the
[WIPI EventQueue specification](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/EventQueue).
A September 2026 replay exposed the missing soft-key conversion: Call reached the
guest as `-10`, which the guest changed to `-8` before calling
`Display.getGameAction`; returning `-8` instead of `92` left its call prompt
visible. With the conversion, the same recorded inputs from a fresh save copy
dismiss the upper-left prompt and gameplay continues. Unmapped keys, including
Call itself, pass through unchanged; unknown reverse actions return zero.
`TestJavaDisplayGameActionsIncludeSoftAndSideKeys` covers the forward/reverse
pairs, and `TestJavaDisplayPreservesKeysWithoutGameActions` guards the fallback.
Replay artifacts remain under `var/acceptance/input-fix/`.

**A `Clip` is a `BaseClip`, as this module shows.** Its class list gives
`BaseClip` the virtual methods `putData([BII)I` and `clearData()V`, gives `Clip`
only `setVolume(I)Z`, and then dispatches `clearData` on a `Clip`. Rooted
separately at `java/lang/Object`, the two vtables overlap: this platform numbered
`BaseClip.clearData` at a slot `Clip` never fills, so the call arrived at an empty
slot. Relating the two classes restores every slot to the module's numbering. The
two methods fill and empty a clip built without bytes, and both drop the mixer's
decode, since appending to a played clip while keeping the old decode plays the
old sound.

**A `Player` call handed a null clip answers `false` rather than failing.** The
same title stops the sound for a scene it never started, and the specification
gives these calls a boolean for exactly a request that cannot be carried out.

<a id="implementation-a-module-that-reads-its-argument-block-before-it-fills-it"></a>

#### A module that reads its argument block before it fills it

**The section above explains this fault.** This is how it looked before the entry
was found, kept because the reading of the registration routine is right and
identified the door as the wrong one.

One module faults in its entry point, before any platform call, on a read of
`0x8`. The first block a module is handed is scratch it fills with a pointer to
its init struct, and this one starts by reading and following word two of that
block: `[[paramOne + 8] + 8]`. Writing a recognisable value into `paramOne + 8`
moves the fault to that value plus eight, settling that it is the first block,
not the second.

**Its use of the word identifies it.** It walks its own table (a count halfword
followed by 24-byte records), comparing each record's first field against the
word, and calls a nine-argument registration routine for the match. The compared
fields point into the module's string pool, at the title's main class name and an
obfuscated one-letter class; the registration arguments are the addresses of
`startApp`, `pauseApp`, `resumeApp` and `destroyApp`. The block carries at that
offset **the name of the class the runtime wants started**, and the module
registers the matching Jlet.

Unknown: where a runtime would get a pointer equal to one of the module's own
strings, since the comparison is by address, not content. The descriptor names
`Clet` rather than either class, so it is not simply the descriptor's main class.
The investigation stopped there.

<a id="implementation-a-screen-that-comes-out-grey-and-the-four-platform-answers-it-does-not-blame"></a>

#### A screen that comes out grey, and the four platform answers it does not blame

One title's slot-selection screen draws its map background in about two dozen
blue-greys while the rest (the parchment panel, sprites, text) is in full colour.
Reported as a "black and white screen", it had never been reproduced; replaying
the reported session's key presses as a route does not reach it, but four
scripted presses do (dismiss the connection dialog, decline the authentication
offer, then start), landing on a screen matching the report pixel for pixel.

This is the title that blits for itself: it writes the framebuffer directly and
makes **no** `MC_grpDrawImage` call over the whole route. That leaves four ways
the platform could be at fault, all measured clean:

| Platform answer | What the run says |
|---|---|
| framebuffer geometry | `MH_fbGetBpp` = 16, `MH_fbGetBpl` = 480 for a 240-wide screen |
| `MC_grpGetPixelFromRGB` | 28,145 calls, all `(r, g, b)` in 0–255, all converted to 5-6-5 |
| resource bytes | every `.gsa` and `.dat` opens and reads at its packaged size |
| allocation contents | filling every non-`calloc` block with `0xa5` changes nothing on screen, so nothing is drawn from memory the title never wrote |

The title also asks for **2,531 distinct colours**, 1,535 saturated, so its
palette is not the screen's grey; the pixels there are the title's own writes from
its own decode.

**Profiling the screen alone names the writing routines.** A route's `mark`
restarts the profile, so marking just before the screen appears profiles the
scene rather than the boot: 36.4% in `0xb810-0xb8be` and 27.0% in
`0xe44e-0xe508`, the RLE sprite blitter named in the table above. Disassembling
the first finds the run's only routine that could desaturate anything:

```
movs r7, #0x75          ; 117
muls r7, r6, r7         ; blue  * 117
muls r3, r5, r3         ; green * 601
movs r3, #0xff
adds r3, #0x33          ; 306
muls r3, r4, r3         ; red   * 306
adds r3, r7, r3
lsrs r3, r3, #0xa       ; (306R + 601G + 117B) >> 10 — luminance
lsls r3, r3, #0xa       ; and the luminance picks a row of a blend table
```

It is a blend whose strength comes from the source pixel's luminance, the shape of
a "dim what is behind the panel" effect. **It does not make this screen grey**:
patching its first halfword to `bx lr` through the cheat console
(`set 0xb810 0x4770 u16`, honoured by the decode cache because every write drops
it) leaves the screen unchanged. The grey is what the sprite blitter drew from the
title's own asset through its own palette.

Every path on this side is clean, and the remaining question is not a platform
one: **what does this screen look like on a handset?** A photograph or capture
from elsewhere would settle at a glance whether this is a stylised map or a decode
fed wrongly here; nothing measurable here can.

<a id="implementation-guest-profiling-and-what-it-says-about-where-the-time-goes"></a>

### Guest profiling, and what it says about where the time goes

`runlgt <zip> -profile report.txt [-profile-folded stacks.txt] [-profile-from tick]`
samples the ARM core and ranks the guest code the title runs. The sampler is
`internal/armcore`'s and the ranking `internal/guestprofile`'s, both shared with
KTF; LGT supplies only what an address *is*.

**Executable addresses are deliberately left unnamed.** A Clet is a stripped ARM
executable with no method names, and naming addresses by ELF section is worse
than nothing: naming every `.text` address collapses the ranking into one row.
The shared region grouping groups only symbol-less frames, as built for nameless
images, so code comes back as the *address range of the running loop*, which is
what a disassembler is pointed at. Everything that is not the title's code is
named: the heap, the stack, the platform's stub area, and the module's data
sections. A sample in the stub area is a platform call in flight, which a bare hex
number cannot convey.

<a id="implementation-the-guest-is-not-spread-out-it-is-one-loop"></a>

#### The guest is not spread out. It is one loop.

Five local titles, three hundred to four hundred ticks each:

| title | hottest regions | share | code size |
|---|---|---|---|
| A | `0x3a550-0x3a698` | **92.3%** | 328 B |
| B | `0x7084-0x7280` + `0x6d8e-0x6e5e` | **93.9%** | ~700 B |
| C | `0xe44e-0xe508` | **87.2%** | 186 B |
| D | three regions | **88.6%** | ~700 B |
| E | three regions | **80.4%** | ~1.1 KB |

Each spends four fifths to fifteen sixteenths of its instructions inside a
kilobyte of code. Disassembling the top two shows the same kind of code both
times:

- B's 75% loop is an **RLE sprite blitter with per-channel blend tables**. The
  outer loop reads a 16-bit token (a terminator, a skip-to-next-row, a run of
  transparent pixels, or a run length); the inner loop, per pixel, indexes a
  palette, extracts the 5-bit red, green and blue of both source and destination
  pixels, looks each channel up in a 32x32 blend table chosen by a per-call
  level, and packs the result.
- B's 18% loop is the same RLE stream with a **general mask-and-shift blend**:
  eight `(pixel & mask[i]) >> shift[i]` terms summed, both tables passed in.
- A's 92% loop is a different engine doing the same work: a clipped rectangle
  **alpha blend** that memcpys a row into a stack buffer, blends each channel by
  two scalar weights, and copies it back. It is the fade; the same title sets the
  context's alpha 619 times a run.

Two consequences point in opposite directions.

**The addresses differ per title, so there is no one routine to recognise.**
These are the games' own engines, not a shared library, and a hook would need a
byte signature per engine, unlike `MC_knlAlloc`'s callers.

**But the working set is a kilobyte**, ideal for translation: a block
translator's per-block cost is paid once and amortised over millions of
executions, and the hit rate is not in question. The dispatch overhead the host
profile measures (`Engine.Run`'s own 24% plus the decode cache's 9%) is paid on
*this* loop, every instruction, exactly the code a translator would relieve.

**What a player gets, measured on the title the question concerned.** A tick is
50ms of guest time, so host milliseconds per tick against that decides comfort.
Driven from a cold start through its notices, authentication failure, character
select and into the opening boss scene (chained lightning pillars, particles, a
full-screen animated boss), the release build spends **7.3ms a tick over the
first 3,000 and 9.0ms a tick over the next 3,000**, the in-game half: **five to six
times faster than the guest clock it drives**. The whole 6,000-tick run costs 49s
of host time for 300s of guest time, and its boot retires instructions at
**6.8ns each**.

**The one uncomfortable number is a single tick**: the scene load after character
select takes 1.27s in one tick, the same tick in every run. A whole scene arrives
inside one tick here, which is why the run summary reports the slowest tick beside
the total; an average would hide the one thing a player would notice.

<a id="implementation-a-java-title-is-the-other-shape-and-it-is-the-slow-one"></a>

#### A Java title is the other shape, and it is the slow one

The five above are Clets. Profiling a **Java** title over a replay of a reported
session (5,915 ticks, driven by the session's key presses) gives:

| | share | what it is |
|---|---|---|
| `0x9ba3c-0x9bd1c` | 36.3% | the title's own |
| `0xe62c-0xe6c0` | 14.1% | ” |
| `0xe1f18-0xe1fac` | 13.5% | ” |
| `[platform stubs]` | 11.8% | the Java call thunks |
| `0xe2194-0xe2390` | 8.6% | the title's own |

Three quarters is still the title's own code, so the working-set argument holds
and no platform call is to blame. New is the **11.8% in the stubs**: a Java title
reaches the platform through a thunk per call, a cost a Clet never pays.

The run costs 150.8s of host time for 295.8s of guest time (25.5ms a tick against
the 50ms a tick represents), twice as fast as the clock rather than a Clet's five
to six times, and a reported session at 10fps sits in that gap. A debug build
costs 167.1s, **10.8%** more, so a player wanting frames back should use the
release build first.

<a id="implementation-that-profile-was-of-the-guest-and-the-host-was-doing-something-else"></a>

##### That profile was of the guest, and the host was doing something else

Every number above is a **guest** profile, saying which guest instructions ran;
it was misread as saying where the host's time went. A host profile of the same
run shows otherwise:

| | share of host time |
|---|---|
| `syncToGuest` | 29.0% |
| `syncFromGuest` | 26.3% |
| the scheduler parking and waking guest threads | ~17% |
| **the whole ARM interpreter** | **4.2%** |

The interpreter was never the problem. **Two thirds of the host's time was
copying the framebuffer in and out of guest memory around every drawing call**
(see `javaDraw`); the fix and its measurements (16.9s instead of 150.9s on the
same route) are in [A Java title's drawing does not synchronise, and a Clet's has
to](#implementation-a-java-titles-drawing-does-not-synchronise-and-a-clets-has-to).

A guest profile answers "what is the title computing"; only a host profile answers
"what is this emulator spending its time on", and the two had been read as one
question for as long as this section existed. `docs/armcore.md` carries the same
correction from the other side.

<a id="implementation-finding-out-what-a-title-was-told"></a>

### Finding out what a title was told

A fault here is almost never at the reporting instruction. A slot answers zero or
an error code, the game stores it, and the dereference happens hundreds of
instructions later in code unaware of the platform call. The fault address names
the game, not the cause.

The platform therefore records its recent calls. `Options.TraceSVC` /
`SessionOptions.TraceSVC` size a ring buffer of the last N calls,
`Client.SVCTrace` / `Session.SVCTrace` read it back, and `FormatSVCTrace` renders
it oldest first. From the CLI:

```sh
wfeature runlgt <game.zip> -ticks 20 -trace 25
```

The dump appears on any failed start or tick, and at the end of a successful run,
since a title reaching a wrong screen is being answered wrongly somewhere and the
ring is the only record of what it asked. Each line carries the category and
slot, the slot's name or `unnamed`, `r0`-`r3` on entry, the result or error, and
**the link register**: the calling address in the game's code, which turns a
trace into a place to look.

A startup fails before `StartSession` can return a session, exactly the run worth
reading, so the trace travels out on a `StartFailure` error rather than dying with
the client.

`unnamed` is a finding, not a formatting gap: it marks a slot this platform
answers without knowing what it is.

**Java slots are named too, from the loaded module.** A static entry or vtable
slot is a number until the module's class tables say what it stands for, so
naming is a client method rather than a table lookup: `javaSlotName`, the compact
form of what a failure at such a slot reports. Without it a Java title's whole
trace reads `unnamed`, because Java titles make almost no C calls, and their
remaining faults are all on the Java side. With it, a screen that will not advance
becomes a list of what the title was asking for meanwhile, which is how the wait
below was read:

```
java 0x2000030 java/lang/Thread.<class>(...) from 0xd7124
java 0x2000034 java/lang/Thread.sleep(J)V(0xa, ...) from 0xd7138
java 0x55 leave a method(...) from 0xd7118
```

Three lines repeat for the rest of the run. The names come from `javaSVCNames`,
the table failures report through, not a second one.

<a id="implementation-where-a-title-is-waiting-in-the-titles-own-names"></a>

#### Where a title is waiting, in the title's own names

Three repeating lines say a title is waiting, not **which of its own methods is
waiting**, the whole question in a megabyte of compiled Java. Two facilities
answer it, both on by default in a debug build.

**Every serviced Java method call carries the caller's address.** A platform
method's debug line now ends `from=0x...`, the link register, an address in the
module's code. Grouping a run's calls by it profiles which of the title's methods
do anything, and the count alone separates a game loop from a stuck screen:

```sh
wfeature runlgt <game.zip> ... 2>calls.txt
grep -o 'from=0x[0-9a-f]*' calls.txt | sort | uniq -c | sort -rn | head
```

**A thread sleeping in one place long enough reports its stack.** The compiled
code keeps an APCS frame (`mov ip, sp; push {…, fp, ip, lr, pc}; sub fp, ip, #4`),
so `fp` points at the saved `pc` and the three words below are the return
address, the caller's stack and the caller's frame. `javaBacktrace` walks that
chain, naming each address by the nearest method body in the class records, to
give the title's own call stack. Inside a platform call the program counter is a
stub, so the innermost frame is named by the link register.

After 500 sleeps from one call site (five seconds of guest time at a spin loop's
ten-millisecond sleep), the wait is reported once:

```
msg="LGT java thread is waiting in one place" sleeps=500
    stack="w.i()V+0xf0 < b.run()V+0x208 < 0x7fff0000"
```

That line is the save-backup screen above, found in one run where reading member
tables and disassembling took several: it names the waiting method and the Jlet's
own loop as its caller, turning "a card that is never pushed" into "a screen
waiting to be painted". The same stack goes into a platform method's failure
report, so an unimplemented call says which of the title's methods wanted it.

<a id="implementation-what-that-wait-is-actually-testing"></a>

##### What that wait is actually testing

Disassembling the named method around the sleep gives the predicate, which says
what must happen for the screen to move rather than what is missing:

```
ldr   r2, [fp, #-0x40]      ; a local the method already held
ldr   r3, [r2, #8]          ; r3 = that object's field at +8 — an array
ldr   r2, =0x01500a30
ldrsh r2, [r2, #0x44]       ; a signed halfword global: the index
ldr   ip, [r3, r2, lsl #2]  ; array[index]
cmp   ip, #0
bne   … sleep(10) and test again
```

A first reading took that for `while (slots[index] != null)`, an object array
indexed by a global short, which aimed the next step at a watchpoint on a
nonexistent address. **`0x01500a30` is not a variable but the field-index out
array**, the seventh argument of the load call (`0x14`), filled at startup. So
`ldrsh r2, [r2, #0x44]` reads a *run constant*, the word index of field-table
**entry 34**, and `[r3, r2, lsl #2]` is one field of one object, not an array
element.

Three debug lines, each added for this, turn that into names:

- `LGT java platform class` now lists the **field table** entry by entry.
  Entry 34 is `d:Z`, a boolean.
- `LGT java class laid out` now carries `entries=first..last`, which class
  answered which stretch of that table. 34 falls in `w`'s 29..38, so the field
  is `w.d`.
- `LGT java class` now prints each method body's address beside its name, naming
  addresses in the other direction. `w.i()V@0xd7048` puts the sleeping `+0xf0` at
  `0xd7138`, where the trace said.

The predicate is `while (this.d) sleep(10)` on a boolean, whose writers a scan
finds. Every access to entry 34 is an `ldrsh` of `#0x44` against a base loaded
from the literal pool, **four in the whole module**:

| Site | In | What it does |
|---|---|---|
| `0xd3078` | `w.<init>()V` | sets `d = 1` |
| `0xd7eec` | `w.l()V` | sets `d = 1` |
| `0xd7148` | `w.i()V` | the wait above |
| `0xd6f30` | `w.b(Lorg/kwis/msp/lcdui/Graphics;)V` | **clears it** — `d = 0` |

The one clearing writer, `w.b(Graphics)`, draws the card's own content: it
dispatches virtual entry 71, `a(Lorg/kwis/msp/lcdui/Graphics;)V` (each card
class's override), and clears the flag on the way out. `d` means "this card has
not drawn yet", and `i()` waits for it to have drawn.

**`w.b(Graphics)` is reached from exactly one place**, not a vtable: `w.paint`'s
literal pool holds its address at `0xd6ca8` and calls it at `0xd6c44`, behind a
test of one of the class's statics. In a stuck session `w.paint` runs (the debug
build's `from=` addresses land inside it) and the guarded call never does. **The
title waits for a card the platform is painting**, and paint takes its other
branch every time.

<a id="implementation-the-gate-and-what-holds-it-open"></a>

##### The gate, and what holds it open

The static `w.paint` tests at `0xd6aec` is the class's own, at `data + 0xd4`, and
the same scan finds its two writers: `w.f()V` sets it to 1 and bumps an adjacent
counter, `w.g()V` resets both to 0. While a notice is up, paint draws the notice
instead of the card. `f` and `g` are virtual entries 63 and 61, and the dispatch
scan gives their call sites: **`g` has two in the whole module**, one in
`w.k(I)I`, the save-backup routine holding the `Network` calls.

`w.k(I)I` is where it breaks:

```
ldr ip, [r7, #0x84] ; mov lr, pc ; bx ip   ; Network.connect()
cmn r0, #1                                 ; did it answer -1?
bne  0xd8384                               ; no: dial the socket
mov  r0, r8 ; …call…                       ; yes: one call
mov  r0, #2
b    0xd8e28                               ; the epilogue — past its own g()
```

**The failure branch jumps past the teardown.** The notice stays up, paint keeps
drawing it, `w.d` is never cleared, and the Jlet's loop parks in `w.i()` for
ever. No platform call is wrong and nothing reports anything.

Two experiments confirm it, each patching one word of guest code in a running
session with the cheat console (its other use besides cheats):

- **Force the gate.** `set 0xd6af4 0xea00004e` makes `paint` always call
  `w.b(Graphics)`. The wait report disappears, flushes rise from 528 to 1351, and
  the title paints its way to its title menu.
- **Force the connect test.** `set 0xd8368 0xea000005` makes `k` take the branch
  for an answering network. It dials `URL.find("socket://…")`, which used to stop
  the session as an unimplemented slot and now answers the specification's
  `SchemeNotFoundException`; the title **catches it**, takes its notice down, and
  reaches the same menu.

The title is blocked not by a platform error but by `Network.connect` answering
`-1`, which the specification says a handset without access answers.
[`network.md`](../network.md) records that decision and why one title's evidence
did not change it.

The recipe outlives this title, because an out-array offset in a disassembly is
this ABI's commonest dead end:

1. read the offset and base from the instruction;
2. match the base against the load call's arguments to identify the table
   (`0x1500a30` fields, `0x150086c` static fields, `0x15007b4` virtual methods,
   `0x150086e` methods, `0x1500874` static methods; this title's addresses, the
   module's own);
3. halve the offset for the entry, and take the name from the debug lines above;
4. scan `.text` for every other `ldrsh` of that offset against that base; the
   following instruction says load or store, sorting readers from writers in one
   pass.

<a id="implementation-the-clet-is-told-when-its-page-goes-away"></a>

### The Clet is told when its page goes away

`MC_cletRegister` takes a table of six entry points, two of them `pauseClet` and
`resumeClet`. They were implemented with a comment calling them the lifecycle a
Host uses when the page is hidden, but **nothing called either**, because that
Host did not exist yet.

It does now: a server parks a game whose socket dropped rather than closing it,
running `pauseClet` on the way out and `resumeClet` on the way back. See
[`session.md`](session.md#implementation-server-sessions), "The game is told it
was parked", for what the three platforms share.

A module leaving either entry at zero is not an error but has nothing to do, and
the platform answers nil. The fixture in `fixture_test.go` declares both and
writes what it was told into one word, which `TestParkingACletTellsIt` reads back
from guest memory: the platform either called the table entry or did not.

<a id="implementation-playing-a-title-from-the-command-line"></a>

### Playing a title from the command line

The trace says what a title was told; these say what it did with it.

```sh
wfeature runlgt <game.zip> -ticks 1400 -framedir out -save dir \
    -key 30:fire -key 760:fire -hold 30 -steps 600000000
```

- `-key <tick>:<name>` presses a key on that tick and releases it `-hold` ticks
  later. The release is scheduled rather than queued behind the press, because a
  game sampling the keypad once a frame never sees both in one tick. Names are
  the shared WIPI ones (`up`, `fire`, `soft1`, digits, `*`, `#`). **Raise `-hold`
  before believing a screen refuses a key.** One title's character select answers
  `right` at a one-tick hold and ignores `OK`; at twenty it selects, loads and
  drops into the world. A screen answering one key but not another calls for this
  first, because a key the title has no code for and a key it never saw look
  identical.
- `-framedir` writes a PNG per presented frame, making a run reviewable: a title's
  whole boot, menu and first minutes read as a contact sheet.
- `-steps` overrides the per-call instruction budget, used to measure the world
  load before the default was raised.
- `-audio out` records what the run played (`out.mid` for MIDI events, `out.wav`
  for sampled ones), checking a sound path without a speaker.

The flags are in [`cli.md`](../cli.md); how to use them (driving a title to a
scene, probing a stuck screen, diffing two builds frame by frame, reading a
filtered call trace) is the third layer of **Testing** below.

<a id="implementation-testing"></a>

### Testing

There are three layers, each existing because the one below is blind to a whole
class of defect; none subsumes the others.

<a id="implementation-1-the-fixture-and-the-slot-tests--go-test-internalplatformlgt"></a>

#### 1. The fixture and the slot tests — `go test ./internal/platform/lgt/`

The fixture is hand-assembled ARM in `fixture_test.go`, with a small literal-pool
assembler so it reads as assembly rather than hex. Its body is ARM, so it opens
with the `bx pc` trampoline a mixed module uses to get there from the Thumb
entry, and its outgoing calls use the ARMv4T `mov lr, pc; bx rN` sequence rather
than `BLX`.

Slot-level tests beside it drive one WIPI C call at a time against a loaded
client, pinning a contract like the resource pair without assembling a module
that calls it. The Java pieces are tested the same way (the try/throw stack, text
objects, resource streams, `arraycopy`'s element widths, and the class object's
static run), each a contract drivable without a module.

**Blind spot: a contract wrong in the same way as the fixture.** Every LGT
contract that proved wrong looked correct here, because the fixture shared the
misunderstanding: a resource read answering a length where zero meant success, a
timer taking its callback from the arming call's parameter, a graphics context
this platform thought it owned. Being ARM where real modules are Thumb sharpens
the limit, which is why five instruction-set faults survived until a real archive
ran: the Thumb entry, ARM `BLX` immediate, `BLX` register, **Thumb `BLX`
immediate**, and **CP15 cache maintenance**. Check a change to the entry path
against `var/games/lgt` by hand.

<a id="implementation-2-the-acceptance-probe--does-every-real-module-still-boot-and-paint"></a>

#### 2. The acceptance probe — does every real module still boot and paint

```sh
WFEATURE_LGT_ACCEPTANCE=1 go test -run TestLocalLGTArchivesBootAndPaint -v ./internal/platform/lgt
```

Every archive under `var/games/lgt` is started, ticked, and required to present a
frame with something lit; a Java app skips with a named reason. It is the
package's only test running a real module and keeps corrected contracts from
drifting back. At nine seconds for twelve titles it is cheap enough for any
platform-surface change.

**Blind spot: anything past the title screen.** Both defects found in the pass
that added this section (every dialogue line rendering as `%.*s`, and two titles
losing their opening sequence to a delete that did not delete) passed it: each
title booted, painted, reached the world and saved, with wrong screens.

The two rungs above now ask two of those questions without a person watching.

<a id="implementation-2a-two-rungs-above-a-first-frame"></a>

#### 2a. Two rungs above a first frame

```sh
WFEATURE_LGT_SUSTAINED_ACCEPTANCE=1   go test -run TestLocalLGTArchivesSustainAFrame -v ./internal/platform/lgt
WFEATURE_LGT_INTERACTIVE_ACCEPTANCE=1 go test -run TestLocalLGTArchivesAnswerAKey -v ./internal/platform/lgt
```

**sustained** is the title still running a window past its first frame, without
error or ending itself. **interactive** is a key changing what it draws: reach a
first frame, wait for the screen to settle, hold and release a key, keep ticking,
and compare. A title still animating at the end of the settle window is reported
*unanswerable* rather than failed, since a change against an already moving
screen would have happened anyway. `WFEATURE_LGT_SUSTAIN_TICKS` and
`WFEATURE_LGT_HOLD_TICKS` widen the two windows when investigating an unexpected
miss.

Keys are tried by route names, resolved through the same table `runlgt` uses for
a route's keys rather than a second copy, so `fire` means the same to a route and
a rung.

**A title may end its first launch, and the rungs must know that.** Several show
the handset's restart notice first: they write their save, tell the player to
start again, and end. Four local archives do it the moment `fire` is pressed,
which at this rung looks like a dead title (both stop the session). From the
command line: the first run ends at the key's tick and leaves a save, and a second
run against the same `-save` directory continues to the end of its budget.

```sh
wfeature runlgt <game.zip> -ticks 150 -save /tmp/probe -key 60:fire   # "ticks": 61
wfeature runlgt <game.zip> -ticks 150 -save /tmp/probe -key 60:fire   # "ticks": 150
```

Each archive's launches therefore share one save directory, an archive ending
itself gets the second launch its notice asked for, and only an ending on that
launch is reported, with the log naming the answering launch, because a silent
relaunch would hide first-launch behaviour from the report's reader. Judging
every title on what this platform treats as an installation launch would measure
the probe's own fresh directory.

The judgment's design is the other platforms', so one grade means the same on all
three; the reasons are in [testing.md](testing.md#implementation-testing), "Two
rungs above a first frame".

<a id="implementation-3-the-scripted-run--what-the-title-does-with-what-it-was-told"></a>

#### 3. The scripted run — what the title does with what it was told

This layer catches gameplay defects. It is a method, not a test, because the
repository does not know what a game should do next.

**Drive it and look at every frame.** `-key` and `-framedir` produce the frames
and `contactsheet` puts them on one page, so a title's boot, menu, character
creation and first minutes read at a glance. Only that shows a scene that never
plays, since no single frame shows an absence:

```sh
wfeature runlgt <game.zip> -ticks 2000 -framedir out -save /tmp/probe \
    -key 40:fire -key 330:down -key 390:fire -key 450:fire
wfeature contactsheet out sheet.png -every 18
```

Give every probe run its own `-save`, never omitted. Without it the run writes
into the real save tree and the next run starts elsewhere, which reads as "that
key does nothing", a wrong conclusion.

**Probe a screen that will not advance rather than guessing.** Run the same key
prefix once per candidate key and compare the last frame: a one-line loop over
`fire soft1 soft2 clear call hangup`, the arrows and digits, `-frame` each, and
compare hashes. Both outcomes are findings: one differing key names the key, and
*every key identical* means the screen is not waiting for one, which sent the
restart-notice title above to its filesystem trace.

That found the confirm key on a character-creation screen (the obvious one; the
run had been ending before the started load finished), and showed two Java titles
recorded as stopped were sitting on a dialog with a confirm softkey. **A title
waiting for a key looks exactly like a stopped one**, and the sweep costs one run
per key. A seemingly stuck run is as often too short, or too quiet, as wrong.

**Diff two builds frame by frame.** Run the same key script against the binaries
before and after a change, and `framediff` the two directories:

```sh
wfeature framediff after/ before/
  tick1023  (67,262)-(76,272)
  ...
4937 of 5798 frames present in both runs differ
```

The first differing tick names what the change did and the bounding box where on
screen (`(67,262)-(237,293)` is the dialogue box alone), turning "I think I fixed
it" into a tick and a rectangle. It also kept a wrong conclusion out of the
dialogue fix: the screen where the defect was first reported came back
**byte-identical** between builds, so the real screen was three hundred ticks
further in.

**Ask what the title was told, filtered.** `-trace-live` streams platform calls as
serviced, reading name arguments as names. Filtering to a slot family makes it
readable, and a wrong answer is usually obvious once the sequence is in order:

```sh
wfeature runlgt <game.zip> -ticks 600 -save /tmp/probe -trace-live fs ... 2>calls.txt
```

The whole delete defect was two adjacent lines of that output. Use it before the
disassembler: it needs no static analysis and shows what the title asked for, not
what it might have asked for.

**Static analysis when the trace is not enough.** `python3` with `capstone`
disassembles Thumb; the module's lazy-import stub table decodes to the complete
set of slots a title uses, without running it. Do not name the script `dis.py`: it
shadows the standard library module `capstone` imports.

Two scans pay off whenever a Java slot is in question:

- **Every dispatch of one vtable slot.** A dispatch is four words,
  `ldr rB, [recv]; ldr ip, [rB, #(slot+1)*4]; mov lr, pc; bx ip`, so scanning
  `.text` for the `ldr ip` encoding followed by the call sequence lists a slot's
  sites across the module, turning "one call site is not enough" into a choice.
  Two rules keep it honest: a base register loaded from a **literal** is an import
  table, not a vtable, and only the *nearest* write of that register counts
  (taking any earlier `ldr rB, [recv]` turned six real sites into sixteen).
- **Every use of one static field.** The platform-filled field-index array is read
  as `ldrsh rX, [base, #entry]` against a base loaded from the literal pool, so
  that literal-pool index gives every site touching one field, and the following
  instruction says load or store. That traced the flag a stuck title polls to the
  one routine clearing it.

**A watchpoint when the question is "who wrote this".** `Core.Watch` records every
guest store to an address with its instruction. Three passes of reasoning about
who could have overwritten a class object ruled out three plausible causes and
found nothing; one run with a watch on the word named the instruction, and
disassembling it gave the answer. Use it first, not last.

Each hit also carries the ordinal of its writer's first and last store, counted
across all watched addresses, so **it answers "who wrote last"**. A host write
clearing a freshly allocated block and a guest store filling it are the same two
facts in either order, so without ordinals a guest-owned field and a field this
platform wipes under the guest look identical. See `ktf.md`, where that separated
a never-written field from one that only looked so.

<a id="implementation-cheats"></a>

### Cheats

`Session.Cheat()` is KTF's engine over this platform's address space. Regions are
labeled `module` / `heap` / `platform` / `stubs` / `stack`; unlike KTF the module
has no fixed base (an ELF keeps the addresses it names), so the module's span is
asked of the loaded module rather than compared against a constant.

`Session.Tick` rewrites frozen values after the game paints, so a cheat wins over
the tick's writes. The CLI attaches the text console with
`wfeature runlgt <game.zip> -cheat`, pacing the run to about real time so typed
commands get a turn between ticks.

The browser reaches the same engine, which for a while it did not despite
everything behind it being built. The server asked the platform-specific
`Session.KTF()` for an engine and answered "the cheat engine is only wired to KTF"
on nil, so a panel over an LGT game refused every operation while the engine was
attached and running. Hosts now ask `session.Cheat()` and
`session.CheatConsole()`, which answer for whatever platform is behind the session
and nil where none does. That indirection paid off again when the MIDP runtime,
which had no address space and answered nil, grew a synthetic one and needed two
lines to reach the same panel (`docs/skvm.md`). Asking for a platform by name hid
the gap: the refusal named the platform asked for rather than the property
needed, so it read as a decision.

<a id="implementation-what-a-saved-table-is-keyed-by"></a>

#### What a saved table is keyed by

`Session.ImageHash()` is the lower-case hex SHA-256 of the module inside the JAR,
the one executable image this platform loads, and the first half of a saved
table's key. The second half is the archive's own hash, filled in by the Host.

The halves are not redundant: **an address is true of the image, not its
container.** One title arrives in several archives (a repack, a re-zip, a copy
with something added beside the module), all loading the same bytes at the same
addresses, so a table made against one serves all. Keying only on the file would
force redoing the work per container; keying on the image finds it again.

A platform whose title is a bag of class files has no single loaded image, so the
source file identifies it instead. That is a real platform difference, not a gap,
so no hash of something arbitrary is invented.


<a id="implementation-startup-after-an-existing-save-2026-09-12"></a>

### Startup after an existing save (2026-09-12)

A reported LGT Java archive displayed its usage notice on a fresh save but failed
during startup on the second launch. The page log and a CLI run over the same new
save both stopped at `DataInputStream` slot 17; continuing exposed four more
missing virtual calls. The shared LGT platform now serves:

- `InputStream` slots 16/17/18: `mark(int)`, `reset()`, `markSupported()`.
  A byte-array stream starts marked at zero, retains subsequent marks without
  a read-limit expiry, and shares its mark and cursor with a data-stream wrapper.
  Other stream sources still report no mark support; reset throws `IOException`.
  Forwarding custom guest stream mark/reset overrides remains unsupported.
- `DataInputStream` slot 21: `skipBytes(int)`, with nonpositive counts returning
  zero and the result limited to the remaining bytes.
- `DataInputStream` slot 29: `readLong()`, reading eight big-endian bytes and
  returning the low/high words in r0/r1. Truncation throws `EOFException`.
- `StringBuffer` slot 21: `append(boolean)`, appending `true` or `false` and
  returning the same buffer.
- `Stack` slot 35: `empty()`, using the existing vector storage.

The contracts were checked against the published CLDC pages for
[InputStream](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/InputStream.md),
[ByteArrayInputStream](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/ByteArrayInputStream.md),
[DataInputStream](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataInputStream.md),
[StringBuffer](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/lang/StringBuffer.md)
and [Stack](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/util/Stack.md).
Slot numbers follow the declaration-order rule documented above, anchored by
existing adjacent slots and the failing call sites; the specification does not
define this vendor ABI.

Regression tests dispatch through the encoded virtual slots, including inherited
stream calls, shared cursor reset, negative skips, truncated longs, both boolean
values and stack push/pop transitions. No JVM bytecode implementation changed.

The first correction completed 6,000 CLI ticks but left an
authentication-progress dialog visible. That did not indicate a missing
authentication adapter: `ServiceJavaThreads` rebuilt its live-worker slice from
the workers present at entry, so a child appended by a running worker was
discarded by the end-of-round assignment before its first grant. The frame loop
survived while the connection thread never ran.

The scheduler now leaves the shared list intact while granting the original
workers their slices, then removes completed workers from the current list.
Children survive to the next round, including when their parent finishes, and
unserviced workers survive an early error so session shutdown can reach them.
Tests cover a parent that parks and one that finishes after creating a child.

With that correction the same save reaches the game-start menu and story
sequence. No authentication adapter or success response was added; the selected
mechanism remains `unsupported`. Local evidence is under
`var/diagnostics/lgt-startup/`; existing user saves were not changed.

The whole-char-array `String(char[])` constructor now uses the existing bounded
character-array constructor path. Continuing through story skip exposed Calendar
slot 28 (`set(int,int)`), slot 22 (`getTime()`), and StringBuffer slots 30/34
(`insert(int,String)` / `insert(int,int)`), now implemented. Calendar field
changes stay pending until read, preserving sequential month/day assignments
across temporarily invalid dates; `getTime` returns an independent Date snapshot.
Buffer insertion uses UTF-16 offsets and validates the insertion range and
resulting length.

Calendar slot 19 had been inferred as `getTimeZone()`. The native callers at
return addresses `0xc5588` and `0x5bbc4` pass YEAR (1), then MONTH (2), DATE (5)
and other field IDs, and consume integers; returning a TimeZone reference
corrupted date fields even during startup. Slot 19 now maps to `get(int)`; slot 14
remains supported for the smaller layout. This corrects the older inference
above. The field and Date contracts were checked against the
[WIPI Calendar specification](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/util/Calendar),
which defines method behavior; the native call sites establish the vendor slot
numbers.

The story-skip CLI route now completes 6,500 ticks, reaches the shop with the
expected March 2 date, and responds to right input: an initial gameplay check,
not a full playthrough or proof of all save/progression paths.

Chromium and WebKit both passed real-page startup over copied existing saves,
confirm input through the menu into the story, story skip into the shop,
directional selection, and reload/reconnection, with zero page errors. An
explicit `-no-auth` CLI control also reaches the menu, confirming that the
scheduler repair, not a selected offline adapter, resolves this authentication
wait.

Final isolated-tree checks passed: `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, and debug/release CLI and server
builds. The changes were copied back to the original working tree and both server
profiles rebuilt there; running server processes still need a restart to use the
new executable.

<a id="implementation-local-binary-authentication-confirmation-2026-09-13"></a>

### Local binary authentication confirmation (2026-09-13)

A fresh native title offered certificate authentication, then stayed on its
authentication-progress screen after the user confirmed the connection. The
baseline trace showed `MC_netConnect` returned zero, its delayed failure callback
ran, and the guest closed the network: the existing asynchronous fix worked, and
this title deliberately keeps the screen on failure.

A controlled successful dial exposed the remaining contract. The guest creates a
TCP socket, connects it through a callback, writes a 60-byte `KP` packet, reads a
four-byte header and then the remaining bytes, dispatches command 12, and closes
the socket; its command-12 handler records completion without a payload. The new
`lgt-offline-authentication` adapter recognizes the connected packet builder,
send/dial/socket path, callback pointers, endpoint and response dispatcher,
rebuilds the exact request from the session identity, handset model and
module-owned application/version strings, and returns the eight-byte completion
frame in process. No OS socket, DNS lookup or external request occurs.

It follows the published
[WIPI C network contract](https://mirusu400.github.io/wipi-wiki/c-api/network.md):
accepted connects complete through callbacks, socket callbacks receive
`(fd, error, param)`, and a read-ready callback is registered after
`M_E_WOULDBLOCK`. Unknown data, endpoints and callbacks still fail. Recognition
uses no archive name, digest, fixed application identifier or fixed code address.

An authored module verifies relocated recognition, disconnected relationships,
mutated code and literals, archive identity, all request-byte mutations,
fragmented writes, bounds and diagnostic opt-out. A local scan selected two
distinct modules among 127 parseable archive copies. The reported fresh-save
route now completes write/read/close, leaves the progress screen, reaches the
guest's next consent prompt and, after an explicit refusal, character information
input. The emulator does not choose consent. Ordinary progress save and a
fresh-session restore remain to be verified separately.


<a id="followup-lgt-compatibility-follow-up"></a>

## Compatibility follow-up (2026-09-12)

Compatibility decisions following the 2026-09-12 audit: what the API scanner
proves, the guest-stream fix, and the evidence from the save and text-input
investigations.

<a id="followup-java-api-coverage-follows-dispatch"></a>

### Java API coverage follows dispatch

A Java member name is diagnostic metadata, not evidence the runtime can execute
the member. Coverage now uses the call path's resolution rules:

- Core Java interface functions count when the Java SVC table has a handler.
- The unnamed class entries in a class's static run count because the handler
  resolves them to the platform class object.
- A static method counts when its class, name, and descriptor select a method
  body. An unnamed static entry may also count when its descriptor leaves one
  unclaimed method body on that class.
- A virtual method first follows the baked vtable slot through the platform
  superclass chain. If no baked slot applies, it counts only when the module's
  metadata identifies a registered method body.
- The known Java auxiliary tables and the Java entry in the OEM table count as
  accepted auxiliary contracts, matching the Java dispatcher.

One resolver supplies both the method execution selects and the name tracing
uses, so an inherited baked method is reported against its declaring class while
keeping the dispatching class. A named metadata member without a body stays
readable in diagnostics and counts as unimplemented.

Regression tests cover inherited and unsupported baked slots, metadata members
with and without bodies, exact and descriptor-only static resolution, unnamed
class entries, and an accepted auxiliary table, each comparing the coverage
result with the executable dispatch result rather than a second list of expected
implementations.

<a id="followup-what-the-import-scan-proves"></a>

### What the import scan proves

The LGT import scan runs `Client.Start` and reports only imports resolved by its
return (see [What a module resolves, and when](#implementation-what-a-module-resolves-and-when)).
Java import stubs may also resolve on first call, and later virtual dispatch
entries come from class metadata, so the report is a lower bound on the surface
reached during boot: zero gaps does not mean later menus or gameplay are covered,
which still needs static metadata inspection and driven traces. A startup failure
narrows it to resolutions made before the failure.

A read-only scan of the local corpus processed 31 valid archives. Four Java
auxiliary entries formerly reported as gaps are accepted contracts and no longer
appear. The remaining startup result is C-library slot `0x32`, resolved by four
archives. Two other inputs failed archive validation before execution for lack of
application metadata. The scan modified no archive or save tree.

<a id="followup-guest-stream-mark-and-reset"></a>

### Guest stream mark and reset

An input wrapper can wrap an application-supplied `InputStream` subclass. It
already forwarded reads, but answered its own host-side `markSupported` flag and
lost the subclass's mark contract.

Opening a guest stream now inspects the inherited `InputStream` vtable slots for
`mark(int)`, `reset()` and `markSupported()`, and wrapper calls invoke guest
overrides when present. Reset discards bytes pulled from the guest's old cursor
before the next read. A block-read override is asked only for the bytes the
wrapper currently needs, capped by the existing pull limit, keeping the guest
cursor at the wrapper cursor when mark is forwarded.

The regression test uses guest Thumb routines whose writes make the forwarded
receiver, read limit and reset call observable. A separate test records the
length given to a guest block read, so future read-ahead cannot silently move the
guest past the wrapper's mark. Existing byte-array mark/reset and
unsupported-reset tests still exercise the host-owned stream path.

This fills the forwarding omission the audit reproduced; it claims no support for
unrelated, unobserved stream ABI slots.

<a id="followup-save-and-menu-evidence"></a>

### Save and menu evidence

The Java and C file implementations share the per-title store, and the driven
evidence in [Every file a Java title opens, from the platform method table](#implementation-every-file-a-java-title-opens-from-the-platform-method-table)
proves open, write, close, persistence and read for the paths titles reached, but
not a progress save: one in-game menu has no save entry (its progress write sits
behind application state), and another title's save action reports that the area
forbids saving. The observed calls justify no further file or menu ABI slot.

A fresh replay on 2026-09-12 in an isolated empty save directory reached the field
and the five-entry in-game menu, then selected and confirmed quit. The live trace
recorded `Jlet.notifyDestroyed()` reaching `MC_knlExit`, the next frame was black,
and the save directory stayed empty: the menu and exit calls are live, with no
evidence of a progress-save call.

A second fresh replay drove 5,920 ticks through the opening to a playable cave and
pressed the title's documented save key. The title showed its own "saving is not
available in this area" message; the file trace has no call caused by that key,
and the isolated tree holds only the five-byte settings file and three
eleven-byte empty slot files created at startup. The route reaches a real save
decision that the application rejects before progress serialization or platform
I/O. A progress round trip still needs another area.

A bounded continuation cleared nearby enemies and followed the visible cave
corridors through two room transitions; after each the save key produced the same
message and no file call. Further straight movement stopped at collision walls.
The packaged map data describes the dungeon's submap topology but marks no
save-enabled room, and the packaged help says only that saving in a boss area
relocates the saved position to the designated return point. Reaching a known
permitted state requires completing more of the opening dungeon or its quest;
there is no evidence-led short route from the current room.

Future save checks should start from the title's packaged help or key map, drive
to a state where that key is active, and verify both the save-tree bytes and a
fresh-session load, distinguishing settings files and pre-created empty slot
files from saved progress.

<a id="followup-screen-orientation-evidence"></a>

### Screen orientation evidence

An additional local LGT archive outside the main scan was examined as a suggested
landscape application. Its descriptor has no screen size or orientation, and,
more decisively, a startup trace with the host forced to 320 by 240 still makes
the module's two OEM configuration calls with `(0, 0x64, 240, 0)` and
`(0, 0x65, 320, 0)`: module values opposite the forced host dimensions.

At the default size the archive paints its usage notice across a 240 by 320
frame. A forced wide run changes the host surface, and the notice can center
within it, but the module's declared dimensions do not change. The archive is
evidence for the existing portrait size, not for automatic landscape selection;
a different edition would need its own module trace or descriptor evidence before
adding a selection rule.

<a id="followup-native-text-input-boundary"></a>

### Native text input boundary

The LGT session now exposes a native-text provider for focused Java LWC TextField
and TextBox components whose parent chain reaches a shown Shell. It snapshots
contents, constraints and lifecycle revisions; complete-string commits revalidate
them, enforce Java UTF-16 limits, and reject stale targets. Shown/focused widget
graphs are collection roots, and lifecycle callbacks resolve exact signatures
through guest inheritance.

The runtime uses the component string-setting contract. An explicitly installed
InputMethodListener remains unsupported, because its per-key composition delta
interface cannot safely represent an arbitrary whole-field replacement. The
adapter adds no complete widget renderer and consumes no historical keypad
composition events.

An authored AOT archive initializes a field through Java imports, class tables,
allocation and virtual lifecycle calls. Shared session and actual WebSocket tests
prove Korean/emoji commits, limit rejection/retry, and reopened readback;
Chromium and WebKit also pass native-dialog composition isolation, key isolation,
reload/resume and length retry. See [native text input](../native-text-input.md)
for artifacts and the distinction between fixture proof and real-archive routes.

The WIPI-C UI component path still provides no target: class lookup and component
creation are refused, so no editable component exists. WIPI-C input-method calls
turn one key into caller-owned completion and composition buffers and retain no
field or whole string for a later Host commit. Text drawn and edited by
application code is opaque guest state. These paths report no supported active
editor and need an established guest editing contract before support can be
added.

<a id="lgt-qa-2026-09-20"></a>

## LGT rendering, input and pacing QA — 2026-09-20

This investigation covers five reported archives. Local filenames are mapped to
case numbers in ignored `build/qa-lgt-paths.json`; tracked evidence uses
archive SHA-256 prefixes only. Original archives and user saves are preserved.
Replay saves are under `var/savedata/qa-lgt-20260920/`.

| Case | Archive SHA-256 prefix | Reported behavior | Final evidence |
| --- | --- | --- | --- |
| 1 | `6b515884dbc13ed8` | Black text backgrounds, map and status bars | Production wide-context replay restores town map, lettering and HP/MP bars. |
| 2 | `34c48bbae783f11b` | Missing lettering and black lower-right area | Production replay restores both initial logos, title lettering and menu without the black corner. |
| 3 | `442a4f59997e7574` | Magenta sprite edges and no sound | Battle replay reproduces mirrored magenta silhouettes; color-key fix removes them. Media-device advertisement restores audio events; Chromium receives them and produces a nonzero Web Audio output signal. |
| 4 | `619d98bc8f64c3d4` | Name editor restricts Host entry to digits | Numeric-mode inference and output-only buffer convention identified and corrected; acceptance below. |
| 5 | `735a579d82ac53bb` | Normal speed only while attacking; no sound | File-mode correction restores settings and music; removing unsolicited key paints gives 50 frames per 5 seconds both idle and attacking. |

<a id="lgt-qa-2026-09-20-name-editor"></a>

### Name editor

The game enters its editor with a Korean default name, then calls
`imSetCurrentMode(3)` from `0x3c227`, which selects `N123` in the returned
mode table. The Host treated the keypad automaton as a field constraint,
selected a numeric keyboard, and rejected Korean before calling the guest. The
[WIPI input-method API](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
defines this mode as an index into the automaton's languages, not a widget
validation rule. Host-composed text now bypasses the keypad restriction;
ordinary numeric keypad events keep their behavior.

That alone does not fix this archive. Its caller at `0x3c4ce..0x3c4f6`
initializes two local five-byte strings, passes them at `sp+0x124` and
`sp+0x11c`, and reads returned lengths at `sp+0x18` and `sp+0x14`, without
initializing those lengths as capacities. This differs from the published
input-capacity annotation and from the existing capacity-taking fixture;
reading the uninitialized size as a capacity rejects even one Korean character
in the observed run.

The compatibility path recognizes that caller's complete argument setup and
checks the live stack pointers. It supplies only the five bytes the caller's
initialization evidences, independent of residual size values, and delivers at
most four EUC-KR bytes per callback, keeping characters intact; further
characters use successive callbacks through the same widget. Unrecognized
callers still use their declared capacities, including zero. No archive name,
module hash, fixed guest address, or blanket zero-size fallback selects this
behavior, and the widget keeps its field-length and validation policy.

Authored tests cover composition in every keypad mode, strict encoding,
unchanged numeric key behavior, stale snapshots and atomic rejection for
ordinary capacity-taking widgets. The output-only caller fixture also checks
instruction mutations, mismatched stack arguments, ARM return addresses, large
residual length words, character boundaries and a byte guard after each
five-byte buffer.

<a id="lgt-qa-2026-09-20-initial-graphics-investigation-superseded-by-the-wide-context-fix"></a>

### Initial graphics investigation (superseded by the wide-context fix)

Case 2's missing text is already visible in its initial connection-error
dialog. The source glyph surface contains ink (163 pixels of RGB565 `0xfe4e`
in the first diagnostic copy). Its destination context at `0x1500360` holds
`0x6ea1` at byte offset 28, which the runtime read as an x translation of
28,321 pixels, drawing outside the display.

Disassembly shows `0x6ea1` is a color-key pixel callback: it compares the
incoming second argument against a key read from guest globals and returns the
first or the second argument. Several engine helpers store callback addresses
directly at context offset 28 rather than through `MC_grpSetContext`, while
the runtime expected an offset there and required callbacks at offset 32
registered through that API.

An ignored diagnostic overlay treating the observed word as a callback removed
the bogus translation, and case 2's initial dialog then displayed its text and
button label (`build/qa-lgt-2-baseline.png`, `build/qa-lgt-2-direct.png`;
context values and disassembly in `build/qa-lgt-2-context.log`,
`qa-lgt-2-disasm.txt` and `qa-lgt-2-helpers.txt`). That confirmed the
mechanism, but the overlay was not merged: whether a word looks like a code
pointer is not an unambiguous layout rule, and the specification treats
`MC_GrpContext` as opaque. Because the callback reads mutable guest globals,
the cache keyed only by function, parameter and pixel pair could not be
assumed valid for it either. "Wide graphics context" below records the
revision-selected layout that replaced it.

<a id="lgt-qa-2026-09-20-mirrored-sprite-transparency"></a>

### Mirrored sprite transparency

Case 3 reaches a battle from an isolated copy of its existing save.
Left-facing sprites keep their shape, while sprites the guest reflects show
magenta silhouettes and missing ink; disabling the pixel-operation cache does
not change this. The guest obtains an image framebuffer pointer, swaps
two-byte RGB565 pixels horizontally through `memcpy`, draws the image, then
reverses the swap. The runtime refreshed those pixels but kept the original
positional alpha mask, so transparency no longer matched the reflected pixels.

Decoded images now retain a color key when every transparent pixel has the
same RGB565 value and no opaque pixel has that value, and drawing tests the
current guest pixels against that key. Multiple transparent colors or a
collision with opaque art, including collisions introduced by RGB565
quantization, keep the positional mask. Opaque magenta remains visible when
the encoding declares it so.

The [graphics specification](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
describes an internal mask but not how it follows raw framebuffer edits. This
follows the observed caller; it does not claim that all encoded alpha can be
reconstructed from RGB565 memory.

The authored reflection test fails before the change and passes afterward,
including restoration of the original pixels; further tests preserve
ambiguous masks. The same battle route loses the magenta silhouettes: compare
`build/qa-lgt-3-cache-battle.png` with `build/qa-lgt-3-keyed-battle.png`.
Replay commands and output are `build/qa-lgt-3-battle.commands` and
`build/qa-lgt-3-keyed-replay.log`.

<a id="lgt-qa-2026-09-20-audio-and-pacing-investigation"></a>

### Audio and pacing investigation

Case 3 asks for `MEDIADEVICES` before constructing its clips. The
[WIPI system-information contract](https://mirusu400.github.io/wipi-wiki/hal/system.md)
defines a comma-separated list that includes `Yamaha_MA3`. The runtime now
advertises that device through both the C and Java property boundaries,
matching the shared SMAF decoder; it does not advertise SMF/MIDI input
support.

The same isolated replay produces no notes without the property and a
5,670-byte MIDI recording with it, over 700 ticks with fire inputs at ticks
150, 400 and 550 (`build/qa-lgt-3-media-production.{log,png,mid}`). The later
battle replay emits both MIDI and WAV. Authored tests read the property
through both boundaries, check short-buffer refusal, create a clip with the
returned device, and deliver a newly authored SMAF note to the Host audio
sink. These establish restored audio events, not audible playback in a
physical browser.

The case 5 server report records 19.4 ticks/s, 16.098 ms average tick cost and
0.97x guest speed. As a snapshot it neither establishes normal combat
animation nor explains input-dependent speed; idle and attacking intervals
have to be measured in the same combat state, and sound needs an actual
playback path. Both are settled in the sections below.

<a id="lgt-qa-2026-09-20-recorded-input-acceptance-and-gates"></a>

### Recorded input acceptance and gates

The debug runtime reaches case 4's first name editor from an isolated save
root. It still observes guest mode 3 but reports Host input mode `text`. After
the default name is cleared, submitting four Korean syllables succeeds through
two completion callbacks; the name is visible in
`build/qa-lgt-4-korean-editor.png`, and OK accepts it and advances to the next
name prompt (`build/qa-lgt-4-korean-confirmed.png`). This proves name entry
and confirmation, not campaign persistence or a physical-phone IME check. The
probe and call log are `build/qa_lgt_cases.go` and `build/qa-lgt-4-probe.log`.

After both input changes, `make test`, `make test-debug`,
`go test -race ./internal/...` and `go vet ./...` pass, with 219 passing Node
tests (`build/qa-lgt-input-{test,debug,race,vet}.log`); `git diff --check`
passes. An earlier gate run picked up diagnostic overlay files with `.go`
extensions under `build/`; they were renamed to `.txt` before the passing run.
After the media advertisement and sprite color-key changes the same four gates
pass (`build/qa-lgt-keyed-{test,debug,race,vet}.log`), and the reflection
regression was observed failing before the graphics change. No user server
restart, deployment, commit or push was performed; graphics overlays remain
diagnostic artifacts.

Case 5 reaches the first controllable cave after advancing the introduction
with repeated fire inputs (`build/qa-lgt-5-field-entry.png`). An idle 100-tick
interval takes 5.002 wall seconds and advances 5.000 guest seconds, with 1.691
seconds of busy work and 5,355,349 guest steps. The thread still requests
200 ms sleeps at return address `0x93ac4`, which rules out a slow Host clock
for this interval without explaining the guest's frame pacing. Idle and fire
traces are `build/qa-lgt-5-field-{idle,fire}-trace.txt`; the recording sink
stayed empty, and no pacing patch was applied from this observation alone.

<a id="lgt-qa-2026-09-20-java-file-modes-explain-the-lost-speed-and-sound-settings"></a>

### Java file modes explain the lost speed and sound settings

Case 5 opens its five-byte settings file in Java mode 4. `File.read` returned
-1 although the isolated save contains `01 01 02 01 01`, because the Java
adapter passed the value directly to `MC_fsOpen`, where 4 means truncate. The
Java enum uses 3 for truncate and 4 for read-write. The published File API
defines the four operations without numeric values; the observed reader and
the reference implementation agree on this enum. No reference source code was
copied.

The settings byte at index 2 reaches the guest helper at `0x8fb68`, which maps
0/1/2 to 200/150/100 ms and stores the result in the class data at offset
0x38; the loop reads that value before `Thread.sleep` at `0x93ac4`. An empty
settings read therefore selects the slowest loop and disables music, while the
Host's extra paint after each key independently explains why input appeared to
speed it up. This is a file-boundary defect, not evidence for changing sleep
durations.

The adapter now translates all four Java modes to the C flags. Authored tests
fail before the fix for both truncation and read-write and pass after it;
existing file-stream tests now use the Java mode number rather than a C flag.
C flag behavior and the save format are unchanged.

A production debug replay on another isolated save copy,
`var/savedata/qa-lgt-20260920/file-fix-5`, reads all five bytes; the loop
requests 100 ms, and the title constructs a Clip and starts Player playback.
The Host emits `build/qa-lgt-5-probe-audio.mid`. The prior runtime log is
`build/qa-lgt-5-file-baseline.log`; the new run uses
`build/qa-lgt-5-probe.log`. All four gates pass after this change
(`build/qa-lgt-file-{test,debug,race,vet}.log`).

The fixed build reaches the same first controllable cave. An idle 100-tick
field interval advances 5.000 guest seconds in 5.002 wall seconds and executes
10,489,268 guest steps (5,355,349 before the fix); the room advances at the
saved 100 ms interval without attack input instead of 200 ms, and the Host
records both MIDI and WAV. Artifacts are `build/qa-lgt-5-file-fixed-field.png`,
`build/qa-lgt-5-file-fixed-field-trace.txt` and
`build/qa-lgt-5-probe-audio.{mid,wav}`. Automatic repaint after key delivery
was still present at this point; the two sections after the next close it.

<a id="lgt-qa-2026-09-20-wide-graphics-context"></a>

### Wide graphics context

Case 2's engine writes pixel callbacks at byte 28. A diagnostic using the
56-byte layout corroborated by the reference implementation restores its
agreement text, title lettering and menu. The layout holds four signed 32-bit
inclusive clip endpoints at 0, foreground/background at 16/20, alpha at 24,
callback/parameter at 28/32, font/style at 36/40, XOR at 44, and signed x/y
translation at 48/52. The C API still exchanges exclusive clip endpoints;
SetContext/GetContext translate them at the boundary.

The runtime selects this ABI through the existing code-revision registry for
the verified native-module digests of cases 1 and 2, not from a coordinate
that resembles a function pointer. Unregistered modules keep the compact ABI.
Arbitrary mixed layouts inside one module are not claimed. No source code was
copied from the reference.

Direct callbacks are accepted only as Thumb addresses inside the loaded
module, and their pixel cache is discarded between draws because the observed
callbacks read mutable guest globals. Authored tests cover the 56-byte write
boundary, signed translations distinct from callbacks, mutable callback data,
exclusive clip round trips, empty clips, and retention of compact behavior.
The production debug replay restores the title and its menu
(`build/qa-lgt-2-wide-title.png`; diagnostic capture
`build/qa-lgt-2-full-title.png`), and `make test`, `make test-debug`,
`go test -race ./internal/...` and `go vet ./...` pass
(`build/qa-lgt-wide-{test,debug,race,vet}.log`). Case 1 also reproduces the
defect and is registered; its production replay restores the town map, text,
and HP/MP bars in `build/qa-lgt-1-wide-production-field.png`, with the input
route and log in `build/qa-lgt-1-field.commands` and
`build/qa-lgt-1-wide-replay.log`.

<a id="lgt-qa-2026-09-20-key-input-must-not-create-unsolicited-paints"></a>

### Key input must not create unsolicited paints

The Java key adapter marked the card dirty after every delivered press and
release, even when the application requested no repaint. Case 5 updates
gameplay inside paint, so repeated input also advanced the simulation. The
adapter now delivers `keyNotify` and preserves the existing dirty flag,
leaving repaint scheduling to the application. The authored regression fails
before the change and passes after it: press/release callbacks receive their
arguments, unrequested paints stay absent, and pending repaint requests
survive key delivery.

The full production route reaches the same cave. Across 100 ticks, idle
advances 5.000 guest seconds with 50 presentations, holding fire advances
5.000 seconds with 50 presentations, and twenty-five press/release cycles
advance 5.000 seconds with 50 presentations in total. Measured wall durations
are 5.002 and 4.995 seconds for idle and held fire. MIDI and WAV are generated
during the replay. Artifacts are `build/qa-lgt-5-pacing.commands`,
`build/qa-lgt-5-pacing-replay.log`, and `build/qa-lgt-5-pacing-field.png`.
The four gates pass after the repaint change
(`build/qa-lgt-pacing-{test,debug,race,vet}.log`).

<a id="lgt-qa-2026-09-20-browser-audio-and-completion-evidence"></a>

### Browser audio and completion evidence

A temporary debug server and a Chromium page use isolated game links and
copied saves; pointer clicks on the actual OK keypad button advance each
archive. The page receives 127 audio events for case 3 and 410 for case 5,
both AudioContexts stay running, and an analyser inserted into the existing
output connection records peak amplitudes of 0.170873 and 0.253627, with no
page errors. This verifies audio transport and nonzero browser synthesis
output, not physical-speaker or phone audibility. The temporary server exited
after the check; the user's server was not restarted.

The first automation attempt used Enter while focus stayed on a page control,
so the game stayed at its notice and emitted no audio; the successful rerun
used pointer clicks on the keypad. Results and screenshots are
`build/qa-lgt-browser-audio-results.json`,
`build/qa-lgt-{3,5}-browser-audio.png`, and the script
`build/qa-lgt-browser-audio.mjs`.

Case 2's production initial-logo captures `build/qa-lgt-2-logo-1.png` and
`build/qa-lgt-2-logo-4.png` both keep their lettering, and the title and
lower-right region are visible in `build/qa-lgt-2-logo-6.png` (route
`build/qa-lgt-2-logo.commands`). Case 1's final field capture includes map
detail, map-name lettering, and filled HP/MP bars; case 3's battle capture
shows clean mirrored sprite edges; case 4's captures show the accepted Korean
name; and case 5's field intervals show matching presentation rates under
idle, held attack and repeated key input. These are the acceptance routes for
the five reported defects, not a claim of complete game compatibility,
campaign completion or physical-phone QA.

All four required gates passed after the final repaint change. The second
graphics revision was then added to the registry, and focused LGT and registry
tests pass again (`build/qa-lgt-final-focused.log`); `git diff --check`
passes. No game archive, original user save, reference source file, or
third-party asset is in version control, and no commit, push, deployment, or
user-server restart was performed.

<a id="lgt-qa-2026-09-20-vector-replacement-crash"></a>

### Vector replacement crash

A later physical-device test ended at 2026-09-20 04:12:16 UTC while a Java
paint callback ran after right input. The failure was the unimplemented
`java/util/Vector` vtable slot 26, SVC slot `0x5ba301a`, called from
`0xbb3c0`, with a Vector receiver, an object reference and index 1. The
preceding report measured about 10 fps and normal guest speed. This was a
guest API dispatch failure, not an audio recovery failure or a server panic.

The established Vector declaration order places `setElementAt(Object, int)` at
26, between the verified `firstElement` (24) and `removeElementAt` (27)
anchors. The WIPI specification's CLDC Vector entry defines replacement at an
existing index and `ArrayIndexOutOfBoundsException` for an invalid index. The
handler replaces the reference in place, accepts null, and raises that guest
exception for negative or out-of-range indexes; it does not grow the list.

The regression fails on the missing registration before the change. After it,
the test invokes slot 26 through the native Java dispatch boundary with
arguments in ARM registers, checks replacement at first/middle/last indexes,
and verifies unchanged size and neighbours and rejected empty/invalid
accesses. This is the LGT AOT Java boundary; no JVM bytecode implementation
changed.

`make test`, `make test-debug`, `go test -race ./internal/...`, `go vet ./...`,
and the focused Vector/Stack checks pass, and the debug server was rebuilt. A
copied-save real-archive replay reached the field, held right for 100 ticks
(5 guest seconds, 50 frames), and continued another 100 ticks without failure
(`build/qa-vector-replay.log`). That route did not reach slot 26 in its trace,
so it is a movement smoke check, not a reproduction of the reported scene; the
slot regression is the direct proof, and confirmation at the original in-game
position remains a physical play check. Original saves were untouched.

<a id="lgt-qa-2026-09-21"></a>

## LGT startup, rendering, and media QA — 2026-09-21

This investigation tracks ten user-reported cases in request order. Archive
identities are SHA-256 prefixes, not native-module compatibility keys.
Baseline: `bb2294f`, debug CLI built from the then-current worktree, 240×320,
fresh isolated saves under `var/savedata/qa-lgt-20260921/`. No original user
saves were changed. Case paths and raw evidence stay in ignored
`build/lgt-qa-20260921/`; raw logs larger than 10 MiB are losslessly
compressed as `.log.gz`. Sections are in investigation order, and an interim
status in an earlier section is resolved by a later one where noted.

<a id="lgt-qa-2026-09-21-cases-and-current-acceptance"></a>

### Cases and current acceptance

| Case | Archive SHA-256 prefix | Report and final evidence |
| --- | --- | --- |
| 1 | `01f05f8231f4e4b2` | Traced error -1001 to a saved subscriber mismatch. Revision-scoped reader compatibility preserves length/checksum validation; production reaches and advances a match through 5,224 ticks using existing saves. Fresh authentication remains limited. |
| 2 | `559d580bc0d758dc` | Wide context and exclusive native clip selected from code evidence. Startup pink logo fixed across 318 captured frames; 2,500-tick gameplay route passes. Party/status popups and six additional menu tabs are readable; the user confirmed the reported contrast problem is resolved. |
| 3 | `c046d43d429b3671` | Logo stall traced to swapped long-array store words. Boundary correction restores the timed transition; production reaches the playable tutorial and accepts directional input. |
| 4 | `2d5cada030045cfa` | Two-stage bounded local certificate exchange implemented. Production reaches menu/item screens; a new session with authentication compatibility disabled restores the guest-written certificate state and reaches the menu. Full gameplay is not established. |
| 5 | `2c80548d61ce2451` | Image-path strip offset confirmed. Existing-save copy reaches campaign dialogue; removing the image-only 24-row addition restores the top HUD and lower dialogue border in production. |
| 6 | `26f1bf941f2e8afd` | Typed stream, active-card removal and cache pressure fixes pass the original failure path. Production completes 5,000 ticks with 3,889 flushes and no runtime error, reaching a continuation prompt; full campaign unverified. |
| 7 | `8f47c8601785e16e` | Supplemental data mounted: 72 files, 717,309 bytes. Original version state and additional images read successfully; campaign dialogue reached. Direct-pixel strip correction restores the lower dialog. Nine production dialogue captures, including two-line text, keep names/text inside their boxes. |
| 8 | `6bc7f65e30221d9f` | Transparency calibration origin corrected for the exact module revision. The same 1,334-tick field route changes from 5,690 magenta pixels to zero; full campaign unverified. |
| 9 | `0fdf45ec169ff0b1` | Full 78-second browser replay has no sampled clipping or browser errors. A confirmed percussion voice-lifetime bug is fixed and browser-verified; the user confirmed the reported sound problem is resolved. Original handset timbre fidelity is not claimed. |
| 10 | `21819777ad5941da` | Context ABI correction restores text and images. Production confirm dismisses the server-error notice, reaches a new-game dialogue scene, and restores an existing save into its village. |

Baseline probes use 120 ticks without input, then 700 ticks with fire at 20,
250, and 500. They classify cases and are not gameplay acceptance. Files are
`case-N-baseline.{log,png}` and `case-N-input.{log,png}`. A zero CLI exit
status does not establish a visible or progressing game.

<a id="lgt-qa-2026-09-21-typed-file-streams-and-card-removal"></a>

### Typed file streams and card removal

Case 6 first fails at File vtable slot 11, called from `0xb16c`, naming
`openDataInputStream()Ljava/io/DataInputStream;`. The
[WIPI File contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/io/File.md)
requires a DataInputStream, and IOException when the file is not open or an
input stream is already open. The adapter now shares the existing file-stream
binding but allocates the typed DataInputStream object, whose vtable the
existing big-endian primitive readers serve. Stream close releases the
binding, so a second close does not advance the file cursor twice and a later
stream can open at the consumed position. The regression first failed on the
missing registration; it now checks the returned class, native typed read,
current file position, duplicate-open refusal, idempotent close, reopen, and
refusal after `File.close`.

The same route then reaches the title and fails when new-game input calls
Display slot 12 from `0x1514`, naming `removeCard(Card)Z`. The
[Display contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Display.md)
returns whether removal succeeded. The single-card display now removes its
active card and cancels its pending paint; a null or unrelated card returns
false without changing either. This is not a general multi-card stack or
notification lifecycle. The authored regression first failed on the missing
registration and now checks those states.

`case-6-stream-fix.{log,png}` records the restored title and the second
failure; `case-6-card-fix.{log,png}` records 1,000 ticks, 737 flushes and no
runtime error, reaching the next in-game instruction screen.

<a id="lgt-qa-2026-09-21-decoded-image-cache-pressure"></a>

### Decoded-image cache pressure

The longer case 6 route fails at tick 1,946 with `LGT surface space exhausted`
inside `Image.createImage`, from `0x15d04`. The allocation-failure collection
frees seven objects and zero surfaces: the decode cache kept its surfaces
after the last Image object was collected and had no eviction path, so a
sequence of distinct images could exhaust the bounded 16 MiB surface region
even after their owners disappeared.

Surface allocation pressure now evicts cache-only surfaces after collection.
Live Image and Graphics owners, including objects awaiting a grace cycle, keep
their surfaces, and every cache alias is removed before the handle is reused.
Ordinary collection still keeps cached images, preserving repeat-decode
sharing, and the memory bound is unchanged. Data-arena pressure does not evict
cached surfaces, because a cache hit can hold a surface handle in a Go local
while allocating its new Image object; a separate regression guards that
construction window.

A regression with a deliberately small surface region fails before eviction
and passes afterward, checking cache aliases, live Image and Graphics owners,
and the allocation limit; another decodes an image again after eviction and
checks every pixel. The final production debug replay, `case-6-final.{log,png}`,
completes 5,000 ticks with 3,889 flushes and no runtime error and evicts 253
cache-only surfaces at the former allocation failure, on a separate copy of
the earlier isolated saves. Its final screen is an in-game continuation prompt:
the recorded startup, card transition and allocation failures no longer stop
that route, which is not proof of complete gameplay or progress persistence.

<a id="lgt-qa-2026-09-21-supplemental-data-inventory"></a>

### Supplemental data inventory

Case 7 contains a nested directory ending in its AID `0002A52B/`, holding 69
`.idam` images plus `config.z` (14 bytes), `down.z` (8 bytes), and `pver.z`
(2 bytes), 717,309 bytes in total. The files are inside the outer archive, so
no download is needed to establish their existence; the inventory does not
prove they are every file the campaign requests.

After the initial restart prompt, the next launch reads the isolated saved
`config.z` and `down.z`. The longer route asks for plain `pver.z`, receives
-12, and creates a new version file: archive lookup recognized plain, `P/`,
and `P/<AID>/` paths, not this extra containing directory. The original
archive and its nested files are untouched. Raw inventory and traces are
`case-7-entries.json`, `case-7-restart.log`, and `case-7-long.log`.

<a id="lgt-qa-2026-09-21-validation-status"></a>

### Validation status

The stream, card, and cache changes pass `make test`, `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...`
(`gate-{test,debug,race,vet}.log`). The card regression also passes after a
final test-only setup simplification, and `git diff --check` passes. No server
restart, commit, or push is part of this investigation.

<a id="lgt-qa-2026-09-21-supplemental-directory-mounting"></a>

### Supplemental directory mounting

The loader now exposes files from one outer `wrapper/<AID>/` directory at the
application's resource root. The AID must match the descriptor, so an
unrelated application directory is not searched, and multiple matching wrapper
roots are rejected as ambiguous. JAR resources, canonical outer paths, and
saved files keep their precedence. The archive bytes are shared read-only,
normal deletion tracking applies to the mounted paths, and directory listings
include their immediate children.

Authored fixtures first failed on missing lookup and ambiguous-root acceptance.
They cover plain and installed-prefix aliases, case-insensitive reads, JAR and
canonical-path precedence, unrelated application isolation, traversal
refusal, save overrides, unchanged archive contents, deletion, and directory
listing.

A fresh-save production replay (`case-7-supplement.{log,png}`) reads the
original 14-byte config, 8-byte download state, and 2-byte `pver.z` and
reaches the character-slot selection screen in 4,500 ticks. This closes the
missing-version-file path; image loading and text alignment are covered under
"Direct framebuffer origin".

<a id="lgt-qa-2026-09-21-blank-rendering-and-context-layout"></a>

### Blank rendering and context layout

Case 10's context at `0x15000d8` holds `0x3839` at byte 28 once drawing
starts, which the compact interpretation read as an x offset of 14,393 pixels,
placing later images offscreen. Disassembly shows the guest storing that
literal at context offset 28 (`0x29aa..0x29ac`). The function at `0x3838`
compares the incoming second pixel with a key from a guest global, returns the
old pixel for the key and otherwise the incoming pixel: a pixel operation, not
a coordinate.

The WIPI graphics specification leaves `MC_GrpContext` opaque. The existing
56-byte implementation is supported by the previously documented runtime ABI
and accepts the observed direct callback stores, and an isolated diagnostic on
that path restores the notice, images, text, and title. The production
registry now selects it only for native-module digest
`ac4d6ce5b68c0972b39aab7dea6944dd9a1ea398eba790d95316778a038cd06d`; unknown
modules and compact contexts keep their behavior.

Artifacts: `case-10-context.log`, `case-10-callback-stores.txt`,
`case-10-wide-diagnostic.{log,png}`. A forced-wide experiment on case 2 also
reached its title, but without an established callback cause it was not
registered from that visual observation (see "Second color report: wide
context proof"); case 8 was likewise not selected without an affected
gameplay route. The existing wide-context pixel, translation, clip, callback,
and unregistered-module regressions remain the contract tests.

<a id="lgt-qa-2026-09-21-direct-framebuffer-origin"></a>

### Direct framebuffer origin

Case 7 does not call `MC_grpDrawString` along the reproduced dialogue route;
its text and artwork are written through direct framebuffer pointers. The
specification's baseline argument therefore does not explain this symptom,
and the C text API was not changed on that assumption.

Module `e418a6186814fdd850d3de53092c1e19fb941741057a646ebfd5ce1267241389`
sets a separate strip field to 24 at `0x12bc` (`movs r3,#24; str r3,[r7]`).
Its visible canvas is 240×296, centered in the reported 240×320 framebuffer.
Drawing adds both the centered y origin (12) and the strip (24) before
multiplying by the framebuffer width (for example `0xa250..0xa270` and
`0xaf70..0xaf86`), so direct content begins at row 36 and its last 24 rows
fall below the screen. Platform fills use their own coordinates.

An isolated diagnostic that changes only the strip initialization to zero
shows, on the same 8,000-tick route, the complete lower dialogue border and
SKIP indicator with the expected 12-row centering margin. This agrees with the
documented handset-strip convention and is not evidence that all LGT modules
need a coordinate shift. The production registry selects
`lgt.direct_framebuffer_origin` only for this exact module; mapped code must
match the six-byte initialization sequence before its immediate changes, and
archive bytes and centered coordinates are kept.

Authored Thumb tests execute the original and corrected initialization, verify
the neighboring screen-handle store, and reject unmapped or unexpected code.
An opt-in original-loader test verifies the correction, unchanged module
bytes, selection independent of descriptor identity, and rejection after one
changed ELF padding byte; it passes with `WFEATURE_LGT_DIRECT_ORIGIN_ARCHIVE`
pointing to the isolated local input.

The diagnostic also reads `img/tile24.idam`, `img/tile25.idam`, and later
character images from the supplemental tree. The replay uses fresh isolated
saves, a 20-tick fire press at ticks 20, 500, 1500, 2500, 3500, 4500, 5500,
6500, and 7500, and ends at tick 8000. The uncorrected comparison is
`case-7-text-trace.{log,png}`; the corrected diagnostic is
`case-7-origin-diagnostic.{log,png}`. Both complete 2,675 flushes with
identical instruction counts, which places the visible change at the
coordinate correction. This verifies the supplied additional-data path and the
observed cropping, not the entire campaign or saved progression.

The supplemental mounting and case 10 registry changes pass all four required
gates (`gate-supplement-{test,debug,race,vet}.log`). The production replay
`case-7-origin-production.{log,png}` reproduces the full lower dialogue and
additional-image reads at the same 8,000 ticks and 2,675 flushes, and all four
gates pass after the origin correction (`gate-origin-{test,debug,race,vet}.log`),
as do the local loader test and `git diff --check`.

<a id="lgt-qa-2026-09-21-image-path-origin"></a>

### Image path origin

Case 5 has a different native drawing implementation. At `0x28a10` its
240/320-pixel-wide display branch stores 24 in the image-strip field
`0x151342c`. Image destination calculation (`0x251d8..0x251f2`) adds this
field before the separate general origin at `0x1513ad4`, while platform
primitives do not receive the image-strip addition. The result is an old
background band above the top HUD and a dialogue image shifted below the lower
screen edge.

The diagnostic replaces only `movs r2,#24` with `movs r2,#0`; neighboring
branches and the general origin are unchanged. The native-module digest is
`a408002a417064d1041adf47b93436eb81b445afbc656c85bcf0127984c2106e`. The
production fix `lgt.image_framebuffer_origin` requires that exact digest and
verifies the six-byte instruction sequence before changing mapped code. The
176-pixel branch keeps its original 20; narrower branches are untouched.

Fresh saves repeat an authentication completion message and do not reach the
menu in this route. A separate copy of the existing debug save does reach the
menu, story selection, and campaign dialogue, and is used for the rendering
acceptance without changing the user's original save. At tick 3,380 the
baseline shows the old background above the HUD and the cropped bottom of the
dialogue; the diagnostic shows the HUD at row zero and the full lower dialogue
border, and the next dialogue advances in both runs.

Artifacts are `case-5-saved-{baseline,origin}.png` at tick 3,900 and
`case-5-origin-diagnostic.{log,png}` for the fresh-save comparison. The saved
route waits 60 ticks, presses fire for 20, waits 700, then repeats fire for 20
and waits 500 six times, reaching the next dialogue at tick 3,900. Authored
Thumb tests execute the wide and 176-pixel branches, verify surrounding code,
and reject unmapped/unexpected instructions. The opt-in loader test
(`WFEATURE_LGT_IMAGE_ORIGIN_ARCHIVE`) verifies exact identity, unchanged
archive bytes, descriptor independence, and rejection after a padding-byte
mutation; both original-loader origin tests pass.

The case 5 production replay (`case-5-origin-production.{log,png}`) completes
3,900 ticks and 779 flushes and shows the next campaign dialogue with its full
lower border and the aligned top HUD, on another copy of the existing save.
The changes pass `make test` (including 226 Node tests), `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...`
(`gate-image-origin-{test,debug,race,vet}.log`), and `git diff --check`. Case
5's separate fresh-save authentication loop is not addressed.

<a id="lgt-qa-2026-09-21-long-array-store-word-order"></a>

### Long-array store word order

Case 3 rendered only two frames after 5,000 ticks (50,040 guest ms). Its
worker keeps sleeping for 10 ms and checking a timer that does not expire. At
`0x7b40..0x7b4e` the guest sign-extends its 32-bit interval into r2 and passes
the low word in r3 to Java interface slot `0xfd`, so the observed 1,000 ms
interval arrives as `(array, 0, 0, 1000)`. At `0x7b70..0x7b7e` it likewise
moves the `currentTimeMillis` high result from r1 into r2 and low r0 into r3.
Slot `0x5b` returns the stored value low-first in r0/r1.

The adapter treated the store arguments as low-first, turning 1,000 into
`0x3e800000000` and reversing the epoch timestamp. The fix swaps only the two
arguments at the interface boundary; array storage, long return registers, and
the internal array helpers stay low-first. This is an observed
compiler-helper ABI, not a WIPI public API signature.

The boundary regression first fails with
`element 0 = 0x3e800000000, want 0x3e8`; it covers a positive interval, epoch
timestamp, asymmetric 64-bit value, and negative value through the store and
load SVC handlers, and the internal storage and bounds regression still
passes. The production replay `case-3-wide-array-fix.{log,png}` completes
3,000 ticks, 30,080 guest ms, and 184 flushes, leaving the logo for a menu's
information page. The unchanged 5,000-tick baseline is
`case-3-extended.{log,png}`, and the caller trace is `case-3-full-trace.log`.
No guest code or save bytes are rewritten.

A later production interactive route reaches the title, main menu, character
selection, intro, and the playable tutorial and accepts directional input
without a runtime error (`case-3-gameplay.png`, tick 5,304). The final
correction passes all four gates (`gate-wide-array-{test,debug,race,vet}.log`)
and `git diff --check`.

<a id="lgt-qa-2026-09-21-further-startup-and-transparency-observations"></a>

### Further startup and transparency observations

The case 1 boundary trace initializes the native root and screen manager,
then posts event `0xa600` from paint on every tick. Its registered event
handler forwards that event, but no framebuffer flush follows. The root and
child state are non-null, and a diagnostic resume call does not change them.
This did not reproduce the reported -1001 screen (see "Case 1: reported
authentication error reproduced"). Artifacts: `case-1-full-trace.log`,
`case-1-state.log`, and `case-1-resume-state.log`. No speculative lifecycle
change was applied.

Case 8's production extended route reaches an intro dialogue after 5,000
ticks and 2,498 flushes. That scene has no magenta background and is not the
reported gameplay scene (`case-8-extended.{log,png}`). Its heavy direct-pixel
workload takes about 624 s of measured CLI busy time for 250 s of guest time,
a bounded observation rather than a browser or handset timing comparison.

<a id="lgt-qa-2026-09-21-gameplay-transparency-follow-up"></a>

#### Gameplay transparency follow-up

Case 8 has a deterministic gameplay route in the ignored `replay8.py` and
`case-8-map-context-commands.json` diagnostics: short confirm presses followed
by the skip key reach the first field at 1,112 ticks, and the 1,334-tick
screenshot contains 5,690 fully magenta pixels. Replays with the compact and a
forced wide graphics context produce the same count, so selecting the wide
context is not a sufficient fix for this scene.

The inner archive contains 766 PNG images. An in-memory scan finds no visible
magenta pixels in them, but many contain fully transparent magenta palette
entries; the presence of transparent magenta in an asset does not show which
drawing operation loses its transparency.

Case 4's outer archive also contains nine supplemental files in a wrapper
whose name includes an application identifier, not the supported unique
`wrapper/<AID>/` directory. A diagnostic run that supplied those nine files to
an isolated save copy still reached the missing-certificate dialog at 976
ticks, so supplemental-file availability alone does not resolve the
certificate path. The run may rewrite supplied files, so their post-run
contents are not evidence of the archived contents.

<a id="lgt-qa-2026-09-21-transparency-calibration-origin"></a>

#### Transparency calibration origin

The first field's direct framebuffer blitter uses a key sampled during native
initialization. It draws magenta with `PutPixel(dst, 0, 0, context)` at
`0x1742`, then copies a pixel through its own framebuffer helper into
`0x1500004`. That helper (`0x167c`) adds 24 rows for the screen, so the sample
reads a different location from the pixel just drawn: the observed key is
`0x0000`, while framebuffer row zero holds `0xf81f` and row 24 holds zero.
Later native transparent copies compare against that sampled key, which loses
black pixels and copies magenta into the map.

The WIPI graphics specification defines drawing coordinates relative to the
context's offset and initializes that offset to `(0, 0)`; it supplies no
24-row default translation. Changing general framebuffer pointers or context
origins would affect other rendering, so exact module SHA-256
`a94d19252edfcee2346cd166c4d4e1f1303dbfce987b50992d51de519b3ac942` instead
selects `lgt.color_key_sample_origin`: the calibration call alone changes its
y argument from zero to 24, with an instruction guard, and its read and write
then agree. Archive bytes and saves are unchanged.

A diagnostic replay confirms the sampled key becomes `0xf81f`, and the
1,334-tick gameplay route has zero magenta pixels instead of 5,690; grass,
river edges and object backgrounds render without pink patches. The authored
regression executes the original and corrected Thumb argument setup and the
real PutPixel handler, then reads the native sample location; further checks
cover unknown instructions and opt-in archive identity, descriptor
independence and unchanged archive bytes. Full campaign progression is outside
this bounded rendering verification.

`make test`, `make test-debug`, `go test -race ./internal/...`, and
`go vet ./...` pass after the calibration change (local `gate-color-key-*.log`),
as does the opt-in archive calibration test. The production CLI replay
(`case-8-map-production.{log,png}`) completes 1,334 ticks with zero magenta
pixels and no runtime error, matching the diagnostic's field rendering.

Case 2's startup trace takes a different calibration path: at `0x358f2` it
calls GetPixelFromRGB with `(255, 0, 255)` and stores the returned `0xf81f`
directly at object offset `0x230`, without sampling a screen pixel. The case 8
correction is therefore not applied to case 2 without separate evidence.

<a id="lgt-qa-2026-09-21-second-color-report-context-comparison"></a>

#### Second color report: context comparison

Case 2 writes the transparent-color parameter at context offset `0x20`
(`0x351aa`) and screen width/height at offsets 8/12 during context
initialization (`0x35614`, `0x3561c`), which conflicts with the compact
runtime layout. A native context stack, however, uses a 52-byte stride
(`0x35946`) while its copy routine requests 60 bytes (`0x35960`), so these
facts alone do not select the existing 56-byte layout.

A controlled 2,500-tick replay, with two-tick fire presses at ticks 20, 250,
500, 750, 1000, 1250, 1500, 1750 and 2000, reaches the same dungeon scene under
the production compact context and a forced-wide diagnostic
(`case-2-abi-{compact,wide}.{log,png}`). The layout mismatch was a lead, not
yet a demonstrated fix, and no case 2 compatibility entry was added from this
comparison.

Capturing every startup flush makes the reported pink logo reproducible
between the notice and the title. With only a two-tick fire press at tick 20,
84 of the first 700 ticks' captured frames contain magenta, the largest count
being 5,792 pixels (for example `case-2-startup-frames/tick0578.png`), as
magenta rectangles around the logo's individual shapes. The previously
sampled notice, later publisher logo and title had missed this transient
scene, which became the bounded acceptance path for the color fix.

<a id="lgt-qa-2026-09-21-second-color-report-wide-context-proof"></a>

#### Second color report: wide context proof

A forced wide-context replay at tick 578 removes the magenta rectangles. The
native image blitter at `0x36438` confirms the fields it consumes: clip words
at offsets 0/4/8/12, operation at 28, key at 32, and separate signed offsets
at 48/52. At `0x3664e` it compares each RGB565 source pixel to the key loaded
from context offset 32, which explains how an incompatible context corrupts a
direct blit even when no platform DrawImage call appears in the trace.

The 52-byte context-stack stride is therefore not the complete layout of the
fields the blitter consumes; its 60-byte copies and the access at offset 52
are preserved native behavior, not grounds to truncate the runtime's wide
fields. Only SHA-256
`d1f59407b0101ff266a06b09c70e826c435691e0f3007ffc1612848c653aca10` selects
this revision's wide context. The native context setter computes `x + width`
and `y + height` at `0x355b4`/`0x355b8`, and its direct blitter treats these
as exclusive ends, so a separate `lgt.wide_exclusive_clip` selection preserves
those words while the earlier wide revisions keep their evidenced inclusive
convention.

The authored regression verifies native-visible clip words, the color-key
parameter, separate signed offsets and platform foreground updates together,
covering the direct/native and API layout boundary rather than treating a
successful build as rendering proof.

In production all 318 captured startup frames contain zero magenta pixels,
against 84 affected frames before the change, and the 2,500-tick gameplay
replay completes without an error (`case-2-final-logo/`,
`case-2-final-logo.{log,png}`, `case-2-final-gameplay.{log,png}`). All four
gates pass (`gate-context-four-*`). The broader report of indistinguishable
colors is examined under "Case 2 contrast follow-up" and "Case 2 menu
coverage".

<a id="lgt-qa-2026-09-21-audio-event-and-browser-signal-inspection"></a>

#### Audio event and browser signal inspection

Case 9's 2,000-tick isolated replay uses two-tick inputs at 20 (fire), 250
(1), 500 (1), and 750/1000/1250/1500 (fire). It progresses beyond the initial
consent screen and switches several clips, and the observed media calls set
clip volume to 40. `case-9-gameplay-audio.mid` records 3,423 MIDI events and
1,547 note-ons over 77,815 ms, with no PCM wave file on this route. Note
velocities range from 14 to 127, with programs 0, 10, 11, 13, 33, 40, 82, 84,
87, 94, 98, 99 and 109 on twelve channels.

The 30–60 s excerpt was replayed through the actual `web/audio.js` PageAudio
in headless Chromium, with earlier controller/program state restored. This
validates the synthesizer's handling of these events, not WebSocket delivery
timing or a physical speaker. The measured output peak is 0.994482 and RMS
0.039784 across 2,113,536 analyser samples, with no browser exception and no
clipping observed in those samples.

The stream briefly reaches 25 active note keys and triggers the browser's
24-voice limit once. At that event (36,889 ms) the oldest key (channel 15,
note 36) has its scheduled note-off at the same timestamp, so the cap event
alone is not evidence of a lost sustain and does not justify raising the
voice limit. Diagnostic data is in `case-9-midi-{events,summary}.json` and
`case-9-browser-audio.json`.

PageAudio uses oscillator families and filtered noise, not the handset's
original FM voices. The measured signal neither proves sound fidelity nor
identifies the user's subjective symptom; clarification was needed to
distinguish timbre mismatch, clicks/dropouts and volume spikes, and no
speculative audio-engine change was made from this run.

<a id="lgt-qa-2026-09-21-certificate-path-subscriber-bound-save-encoding"></a>

#### Certificate path: subscriber-bound save encoding

Case 4's archived `savedata` is not identical to a fresh runtime save: both
are 2,720 bytes, but 2,709 bytes differ when compared before launching the
archived copy. The earlier post-run comparison had observed a rewritten file
and could not establish the original contents.

Native save I/O explains why this matters. Routine `0x832cc` copies ordinary
state at `0x150a37c` to the work buffer at `0x150b350`, encodes it, and writes
680 words. Routine `0x8337c` reads that buffer, decodes and checks it, and
copies to live state only when validation succeeds. The codec at `0x83420`
obtains `PHONENUMBER`, builds a CRC table using polynomial `0xedb88320` with
signed right shifts, and XORs each payload byte with the sum of a lookup byte
and a repeating subscriber-number byte. A seed byte sits at payload index
2715, followed by four CRC bytes; encoding and decoding advance the seed for
each byte, and the phone string participates in both paths.

Supplemental availability alone therefore cannot establish that a supplied
save is valid for this runtime's subscriber number. This is evidence for a
subscriber-bound save contract, not permission to rewrite original saves or
skip certificate validation. Original archives and user saves remain intact.

A 2,000-tick production replay confirms the consent path calls `NetConnect`
with callback `0x2d5c1`, and the runtime's offline failure returns the guest
to the missing-certificate dialog. A bounded diagnostic overlay then let only
that exact module/callback use the in-process socket machinery, without an OS
socket and without any successful authentication reply.

The first request is 73 bytes: big-endian length `0x0049`, bytes
`00 00 00 03 f9 00 00 02 05`, version text `V.1.0.1`, padding, the emulator
subscriber number, and zero-filled trailing fields. The native connection
callback is `0x2d471`; the read callback at `0x2d171` first asks for two
bytes, then derives the remaining frame length from that header. The write is
observed at `0x2d8c5` and was rejected after capture. This binary protocol
differs from the already-supported handshake. Artifacts:
`case-4-network.{log,png}`, `case-4-request.log`, and the ignored
`net4-observe.json` overlay; no diagnostic network behavior entered product
code.

<a id="lgt-qa-2026-09-21-certificate-response-dispatch-and-second-request"></a>

#### Certificate response dispatch and second request

At the first write, the native network route (`0x150092c`) is 8 and its
suppression flag (`0x1503ea4`) is zero. Completed frames pass through
`0x2d2e4`, `0x7f730` and `0x7fa74` to route 8's handler `0x74c2c`, which
switches on response byte 3. A five-byte diagnostic frame `00 05 00 00 00`
therefore exercises the command-zero branch, not a certificate grant, and
triggers the native second request builder at `0x7e788`.

The second request is 79 bytes: `00 4f 00 14 00 0c 04 00`, then the
emulator's name in a 50-byte field and trailing zero fields. The response
parser at `0x7e874` reads a result byte at offset 5, a big-endian string
length at 6–7, and that many bytes from offset 8. The diagnostic response
`00 08 00 14 00 00 00 00` exercises this parser with an empty string. Both
frames are read as a separate two-byte header followed by their body.

These probe responses do not establish the real service's success contract:
the 1,700-tick replay still shows the consent/dialog path, with no
certificate issuance, persistence or gameplay access, and empty response data
is not an authentication implementation. The diagnostic has no external
socket and changes only an isolated save root. Artifacts:
`case-4-request-route.log`, `case-4-stage-two.log`, and
`case-4-empty-reply.{log,png}`.

<a id="lgt-qa-2026-09-21-case-1-reported-authentication-error-reproduced"></a>

### Case 1: reported authentication error reproduced

The production CLI with a copy of the existing debug save directory reaches
the title and authentication consent dialog; the source save directory is
unchanged. A deterministic interactive route steps 1,500 ticks, then repeats
seven two-tick `fire` presses each followed by 250 ticks, and the fourth/fifth
captured transitions reach the reported `Error code :: -1001` screen
(`case-1-confirm-commands.json`, `case-1-confirm-route.log`,
`case-1-confirm-4.png`). The same route against a fresh isolated save
directory stays black (`case-1-fresh-confirm.log`, `case-1-fresh-confirm-6.png`),
a separate startup condition.

The guest calls `netConnect` with callback `0x6cc05` and parameter
`0x15078e0`, and the immediate return is zero. The callback at `0x6cc04`
tests its result and forwards a nonzero result to `0x6ca28`, which closes the
connection and invokes the object's failure method, so the offline callback
reaches the authentication failure path. The -1001 screen was at first
conflated with that failure; "Save subscriber comparison" below establishes a
separate subscriber mismatch as its cause. The ordinary WIPI asynchronous
failure contract is unchanged.

An ignored build overlay that recognizes only this module hash and dial
callback accepts the dial and socket in the in-process service and records
the first guest write before rejecting its unknown protocol; it opens no OS
socket and supplies no success response. The request is 71 bytes, beginning
`47 00 06 01`, whose first word agrees with a little-endian packet length, and
carries identity/model/version fields and binary trailing fields. It differs
from the existing `KP` authentication exchange and from the case 4 big-endian
request. `net1-observe.json`, `case-1-local-request.log`, and its screenshots
retain that diagnostic, which still ends on the error screen.

A preliminary isolation probe copies each of the five nonempty original save
files separately into an otherwise fresh directory; each produces zero flushes
over 500 ticks without input, which does not identify the combination or
initialization state that permits startup. No production code changed in
this investigation.

<a id="lgt-qa-2026-09-21-case-1-save-isolation-and-response-parser"></a>

#### Case 1: save isolation and response parser

A leave-one-file-out replay copies the existing saves, omits one file, and
runs 800 ticks with `fire` at tick 250 held for two ticks. Omitting
`game_o.sav` or `game_rg.sav` gives zero flushes; omitting `audio.adt`,
`game_nr.sav`, `speed.dat`, or `.removed` gives 400. The archive also contains
the first two files with the same lengths (2,776 and 294 bytes), but they
differ from the existing saves in 2,602 and 273 bytes, so the fresh run reads
packaged defaults rather than missing these files. The guest
validation/initialization difference is unresolved
(`case-1-save-omission.json`).

The network object's vtable is `0xd2744`; its ARM adjustment thunks subtract
`0x20` from the receiver and call the Thumb handlers. The response handler is
`0x698f8`; `0x6cd00` reads a little-endian total length and subtracts the
four-byte header. The handler reads the command from bytes 2–3 and a signed
status byte at offset 4. Commands `0x101`, `0x103`, and `0x107` copy 40 bytes
from offset 5 into the guest's certificate-writing helper `0x692e4`, and the
flag at offset 45 selects the next UI state; command `0x201` selects state
`0x12`. This is observed private application protocol, not a WIPI network
API contract.

An ignored diagnostic responder returning a 46-byte `0x107` frame with status
zero, a clearly synthetic local diagnostic token, and flag zero reaches the
privacy-consent text instead of the -1001 screen, and the guest writes the
resulting 60-byte `audio.adt` into the isolated save directory. This is not a
carrier certificate or production support. After scrolling down and
confirming consent, the guest sends a second 20-byte request with command
`0x200` on the same connection; the attempted five-byte `0x201` response still
leaves the sampled screen on the connection message, and later state
transitions and persistent acceptance are not established.

Artifacts: `case-1-local-reply-4.png` shows the consent text;
`case-1-local-after-commands.json` records scrolling and confirmation;
`case-1-reply3.log` and `case-1-reply3-after-4.png` retain the unresolved
second exchange. The latest `net1-observe.json` overlay includes both
experimental responses. No production Go code changed during these probes.

<a id="lgt-qa-2026-09-21-save-subscriber-comparison"></a>

### Save subscriber comparison

The guest save reader at `0x69ccc` validates the total length, decodes the
payload, and compares a 12-byte saved subscriber field with `PHONENUMBER`.
Only that comparison's failure at `0x69d9c` loads error -1001; the following
checksum failure uses -1002. The production boundary trace confirms equality
for the initial options/progress reads and nonzero comparison results at
lines 438,695 and 438,760 before the error screen, so files loaded later can
fail even when the startup options match the current identity. The earlier
inference that the network refusal caused -1001 was incomplete.

The diagnostic response work also found that an immediate in-process reply
reenters the guest parser before its caller finishes setting the waiting
state. Queuing the reply until the next service boundary lets consent finish
but does not resolve the saved subscriber mismatch; those experimental
responses remain ignored diagnostics and are not shipped.

`lgt.save_subscriber_identity` selects the observed complete module digest.
With automatic authentication compatibility enabled, the session verifies the
comparison/failure instruction window and changes its conditional branch to an
unconditional branch to the existing checksum validation. File bytes, the
subscriber property, network behavior, and the length and checksum checks are
unchanged; disabling authentication keeps the original branch, other module
revisions do not match, and the session reports `lgt-save-subscriber`.

An isolated diagnostic build with only that branch correction reaches an
actual match from the existing save copy (`case-1-save-identity-after-0.png`)
without synthetic network replies. Fresh saves leave the black startup screen
and reach the guest's authentication-failure notice
(`case-1-identity-fresh.png`), which is not proof of fresh gameplay. Authored
Thumb regression tests execute both identity outcomes and both checksum
outcomes, verify that the checksum result stays authoritative, reject
unexpected code, and compare the surrounding bytes; the opt-in local archive
test checks enabled/disabled session selection and original archive bytes.

In production, the release CLI without any diagnostic overlay reaches and
advances the match through 5,224 ticks from a fresh copy of the original debug
save directory, with multiple changed frames and no runtime error
(`case-1-production-identity.log`, `case-1-production-identity-after-4.png`).
With an empty save directory the same route reaches the authentication-failure
notice and the guest exits at tick 1,795: the saved subscriber mismatch and
existing-save path are fixed, not every fresh-start authentication flow.

`make test`, `make test-debug`, `go test -race ./internal/...`, and
`go vet ./...` pass (`gate-save-identity-{test,debug,race,vet}.log`). The
opt-in archive check passes with an absolute archive path; a relative path
could not locate the input from the package directory. `git diff --check`
passes, and no diagnostic network responder is in the production binary.

<a id="lgt-qa-2026-09-21-case-4-deferred-replies-and-existing-save-comparison"></a>

#### Case 4: deferred replies and existing-save comparison

Because case 1 exposed a synchronous-response state overwrite, the case 4
two-frame diagnostic was repeated with replies queued until the next
socket-service boundary. The run reaches 2,288 ticks and repeats the same
73-byte handshake and 79-byte follow-up, and the missing-certificate prompt
and call-charge consent remain, so response timing alone does not fix the
attempted exchange. Artifacts: `net4-deferred.json`,
`case-4-deferred-commands.json`, `case-4-deferred.log`, and
`case-4-deferred-2.png`/`case-4-deferred-6.png`.

Production replays on isolated copies of both existing debug and release
saves also reach the missing-certificate prompt after 1,000 ticks with confirm
at ticks 20, 250 and 500; neither profile's save supplies a working
certificate, and both source trees are unchanged
(`case-4-original-{debug,release}.{log,png}`). Production code was unchanged
for these probes.

The route-8 dispatcher at `0x74c2c` recognizes commands 0, 1, 12, 20, and
50–53. Command 0 starts the observed account/device-information request, and
commands 12/20 parse a result and optional message before returning to UI
state 4. Empty data through those branches was not shown to write a
certificate.

<a id="lgt-qa-2026-09-21-case-4-certificate-flag-and-nonempty-response-proof"></a>

#### Case 4: certificate flag and nonempty response proof

A write watch on the dialog selector `0x15116fc` identifies `0x81952` as the
instruction selecting message 40 (the missing-certificate prompt), which is UI
state 6. Its caller at `0x86090` reads a certificate-present word at
`0x1509dfc`: zero selects state 6, nonzero state 5. The network response
handler at `0x74cee` writes one to exactly this word only when the parsed
message length is positive, which is why empty diagnostic messages could not
pass the gate although both requests and parsers ran.

The flag is initialized to zero at `0x81ef0`, restored through the 32-bit save
field reader at `0x82664`, and serialized through the field writer at
`0x82d96`; these are the guest's own packed-save paths, so no direct save
rewrite is needed to exercise the protocol.

An isolated diagnostic replay changes the second reply to command 20, result
zero, and the nonempty ASCII message `Handled locally. Nothing sent.`, keeping
big-endian frame and message lengths, the first reply, the request sequence
and deferred delivery, with no external socket. The guest passes the
certificate prompt, returns to the title, opens the game menu, and enters an
item screen by 2,288 ticks (`case-4-message-6.png`, `case-4-message-8.png`,
`case-4-message.log`). This proves the observed certificate gate can be
completed by this local exchange.

The observed socket contract uses dial callback `0x2d5c1`, connect callback
`0x2d471`, address word `0xfa4273d3`, and port word `0x1d3b`. Production
support had to match the known module and exact session request fields,
enforce the two-stage sequence and fragmented-write bounds, keep unknown
operations failing, respect the authentication opt-out, and add regression
and real-session validation; the broad ignored probe accepted request lengths
only. Artifacts: `cert4_watch.go`, `case-4-message-watch.log`,
`case-4-certificate-watch.log`, and `net4-message.json` under the ignored QA
directory. No diagnostic network code entered the tracked implementation.

<a id="lgt-qa-2026-09-21-certificate-message-compatibility"></a>

### Certificate message compatibility

Production selects `lgt.certificate_message` only for the evidenced full
native-module SHA-256 and matching application descriptor. The session's
subscriber number and handset model construct two exact bounded requests;
version, application header, command, field widths and padding must also
match, and the socket contract additionally checks the dial/connect callbacks
and endpoint. No OS socket is opened. Unknown revisions, reordered commands,
changed fields, trailing bytes and uploads receive no success response. The
ordinary authentication opt-out leaves this responder disabled; the selected
session status is `lgt-certificate-message`.

Each socket owns its request stage, fragments and pending response, and
replies become readable only at the next socket-service boundary, so a native
response callback cannot run inside the send routine before its UI state
transition finishes. Closing a socket drops its pending reply and callbacks.
The compatibility layer rewrites no original archive or save; the guest owns
its normal packed-save writes.

The first reply acknowledges the handshake; the second carries a bounded
nonempty informational message stating local handling. The guest's parser
sets its certificate-present field and follows its existing save path. No
carrier credential or remote account is represented. Authored regressions
cover every request split, changed request bytes, sequence/size rejection,
deferred read readiness, two-part frame reads, and cancellation before
delivery; existing local protocol tests pass unchanged.

In production the release CLI without overlays reaches the menu and item
screen by tick 2,288 (`case-4-production.log`, `case-4-production-6.png`,
`case-4-production-8.png`), and the guest writes its ordinary save. A new
process on a copy of that save with `-no-auth` reaches the menu by tick 776
without the missing-certificate prompt (`case-4-restart.log`,
`case-4-restart-2.png`). Because the responder is disabled in that run, this
establishes guest-save persistence of the certificate state rather than a
repeated handshake. Full gameplay and later progress saving are not claimed.

All required gates pass (`gate-certificate-message-{test,debug,race,vet}.log`),
as does the opt-in real-archive session selection/opt-out test
(`gate-certificate-message-archive.log`) and `git diff --check`. Original
archive and debug/release save directories were not modified, and no server
was restarted.

<a id="lgt-qa-2026-09-21-rendering-follow-up-new-game-restored-map-and-dialogue-bounds"></a>

### Rendering follow-up: new game, restored map, and dialogue bounds

Case 10's server-error notice does not prevent progress: the next confirm
dismisses it. Two production routes start from empty saves and from a copy of
the original debug saves; each advances 100 ticks, then performs nine
two-tick confirm presses 200 ticks apart. At tick 1,918 the fresh route shows
the opening scene with readable dialogue, and the existing-save route shows
the restored village and player rather than a black framebuffer; the
intermediate title screen has its lettering and menu. Evidence:
`case-10-fresh-route.log`, `case-10-existing-route.log`,
`case-10-fresh-8.png`, and `case-10-existing-8.png`. This verifies the
blank-rendering path after the context correction; it does not claim that
remote services work.

Case 7's production build repeats the 8,000-tick intro route and advances
through eight further short confirm presses. All nine captured dialogue frames
keep speaker names inside their name boxes and text inside the lower dialogue
border; the two-line dialogue in `case-7-dialogue-4.png` stays above the lower
border with the SKIP indicator visible, and `case-7-dialogue-8.png` stays
within the box. The run ends at tick 9,616 without an execution error. This is
visual acceptance for the reported dialogue placement, without changing native
font baselines or claiming the whole campaign. Artifacts:
`case-7-dialogue-commands.json`, `case-7-dialogue.log`, and
`case-7-dialogue-{0..8}.png`. No runtime code changed for these checks, and
original archives and saves are untouched.

<a id="lgt-qa-2026-09-21-case-2-contrast-follow-up"></a>

#### Case 2 contrast follow-up

The production release replay reaches the dungeon and opens PARTY, the
already-equipped notice, and the selected member's status popup. White labels
on dark panels and black labels on the light notice/status header are
readable, and the status popup renders its HP/SP numbers and centered body
text. This is bounded evidence, not proof that every reported low-contrast
scene is fixed.

`case-2-menu-commands.json`, `case-2-tab-commands.json`, and
`case-2-nav-commands.json` preserve the input routes, with logs and PNGs under
the ignored QA directory; the last route exits normally at 3,732 ticks.
Repeated right inputs in PARTY select member slots, not the top icon bar, so
those captures do not cover different menu tabs. Original saves are untouched.

<a id="lgt-qa-2026-09-21-percussion-source-lifetime-follow-up-2026-09-23"></a>

#### Percussion source lifetime follow-up (2026-09-23)

A full 0–78 s browser replay of the recorded MIDI stream produced no browser
errors, with a sampled peak of 0.975881 and RMS 0.032299 across 5,572,608
analyser samples (`case-9-browser-audio-full.json`). Analyser polling does not
capture every output sample, so these numbers do not prove that no transient
clips, and they do not test WebSocket delivery or physical playback.

The event stream contains 120 same-key percussion retriggers less than 200 ms
apart. A percussion source lasts 200 ms, and its old `onended` callback
deleted the channel/note entry unconditionally, so an old source could remove
the newer source from voice accounting; the original browser replay observed
24 such replacement collisions in the 30–40 s excerpt.

`PageAudio` now removes the entry only if it still owns that exact voice. An
authored test retriggers a percussion key after 46 ms, ends its older source,
and checks that the newer voice and an unrelated key stay tracked; it fails
before the change and passes afterwards, and completion of the newer source
still removes its own entry. The browser replay after the change observed 35
replacement collisions and zero lost replacements, with no browser errors
(`case-9-browser-drum-fixed.json`). Timing and random noise differ between
runs, so collision counts and signal peaks are not a controlled audio-quality
comparison.

This corrects voice accounting, not the oscillator/noise timbre
approximation. Focused audio/settings tests and `make test` pass
(`gate-drum-after.log`, `gate-drum-test.log`); no Go code changed in this
follow-up.

<a id="lgt-qa-2026-09-21-case-2-menu-coverage-2026-09-23"></a>

#### Case 2 menu coverage (2026-09-23)

Backing out of member selection twice permits horizontal navigation between
menu tabs. `case-2-mainmenu-commands.json` records a production release run
ending normally at 4,290 ticks, and captures `case-2-mainmenu-1.png` through
`-6.png` cover ITEM, QUEST, COLLECTION, SERVER, SYSTEM and STATUS. Black text
is rendered on light tan panels and white text on dark headers and gauges;
quest entries, collection choices, system choices, member names and HP/SP
numbers are visible, and the dim rank label in STATUS is distinguishable from
the adjacent name and level. None of these captures reproduces black text on a
black panel, and they do not identify the exact scene the user described. No
color substitution was introduced without evidence of an incorrect color.

<a id="lgt-qa-2026-09-21-requirement-audit-before-user-confirmation--2026-09-23"></a>

### Requirement audit before user confirmation — 2026-09-23

The scope remained all ten reports. The original startup/rendering failure
paths for cases 1, 3, 4, 5, 6, 7, 8 and 10 had post-fix runtime evidence,
without whole-campaign acceptance: case 1 uses an existing save, case 4 proves
certificate persistence and menu entry, and case 6 reaches a continuation
prompt after 5,000 ticks. Fresh remote services and untested later
progression are outside those paths.

Case 2's pink-background defect was reproduced and corrected, but its
black-on-black report stayed unconfirmed after the dungeon, PARTY,
status/notice popups and six other menu tabs; the exact screen or input
sequence was missing. Case 9's browser run and percussion lifetime regression
could not establish that the original audible complaint was gone; a scene or
playback point distinguishing timbre mismatch, clicks/dropouts or an overly
loud part was missing. The user was asked for these details.

<a id="lgt-qa-2026-09-21-user-acceptance"></a>

### User acceptance

On 2026-09-23 the user confirmed that case 2's contrast and case 9's sound
now behave normally, closing the remaining acceptance gap for the ten reported
defects together with the runtime evidence above. The confirmation does not
assert exact handset audio fidelity, working remote carrier services or
full-campaign coverage. The user requested a pull request with these fixes.

<a id="lgt-qa-2026-09-21-pull-request-validation"></a>

#### Pull request validation

After the final percussion fix and user confirmation, `make test`,
`make test-debug`, `go test -race ./internal/...`, and `go vet ./...` pass
again (`gate-pr-{test,debug,race,vet}.log` in the ignored QA directory).
Changed Go files pass `gofmt -l`, and the staged diff passes
`git diff --check`. Original saves, game archives and unrelated local
investigation documents are excluded from the pull request.

<a id="lgt-qa-2026-09-21-existing-download-state-recovery--2026-09-23"></a>

### Existing download-state recovery — 2026-09-23

A later user check passed the other cases but still hit case 7's
additional-download prompt. The existing debug save had an eight-byte
`fs/down.z` filled with zeroes, shadowing the archive's completion/count
record (little-endian words 1 and 69). The supplemental files were present and
mounted, and confirming the prompt attempted the discontinued remote service.
The fresh-save acceptance had not covered this stale saved override.

A copied-save replay reproduced the prompt, and restoring only `down.z` from
the archive's unique matching AID directory let the same input route reach the
game menu, character selection and stat allocation, with configuration and
version files unchanged. The actual debug save directory was backed up under
`var/savedata/qa-lgt-20260921/`, then only its eight-byte `fs/down.z` was
restored atomically from the archive; before/after hashes confirm every other
file is unchanged. The ignored `case-7-download-repair.json` records the
backup location and changed path, and replay logs/PNGs use
`case-7-{user-download,download-repaired}` prefixes.

A new game session must be started after the repair so cached runtime file
state is discarded; no server restart or online download is needed. This is a
targeted recovery of a verified stale state record, not a rule to replace
arbitrary saved download metadata or bypass missing archive resources.

<a id="lgt-key-callback-hang-2026-09-23"></a>

## LGT key callback hang — 2026-09-23

A new gameplay report reopened case 3 of the
[ten-case QA](#lgt-qa-2026-09-21) after the logo-transition fix. The debug
server used one CPU core while its status endpoint still answered HTTP 200 in
0.6 ms. Native sampling placed execution inside
`deliverJavaKey -> callJavaCardMethod -> Core.Call -> handleJavaSVC`. The last
delivered event was a key release (type 2, key -5), followed by repeated
helper 0x55 calls that grew the server log beyond 3.7 GB.

The recognized native module's key handler at 0x8e0c waits at 0x8ee6–0x8ef4
for the class static field at offset 0x5c to clear, calling helper 0x55 in the
loop. Event delivery precedes worker service in the session tick, so a worker
this callback needs cannot run until the callback returns. An authored ARM key
callback waiting on a worker reproduces an instruction-limit failure in the
existing runtime.

Debug report requests also run on the session's command loop after Tick
returns, which is why the page could not save a report during the hang. The
three-billion-instruction session ceiling makes this failure very slow to
report, especially when debug logging writes every repeated call.

The live process was suspended with SIGSTOP to stop log growth while
preserving its memory; it was not terminated or restarted. Native samples, the
bounded key-boundary excerpt and input timings are under the ignored
`build/lgt-hang-20260923` directory. Original saves and archives are
untouched.

The fix services eligible workers after 64 compiler checkpoints inside key
callbacks, preserving synchronized regions and the original step ceiling.
Worker execution remains serialized. Each worker keeps its own try regions,
jump-buffer pool and native call depth across slices, and the suspended
callback's exception state is restored afterwards. Without that separation
the first scheduling prototype hit a missing-try-region failure in the real
archive, so it was not kept as a standalone fix. Current behavior:
[LGT](../lgt.md#key-callbacks-waiting-on-workers).

<a id="lgt-key-callback-hang-2026-09-23-verification"></a>

### Verification

The authored ARM callback regression fails before the change at the
one-million-instruction ceiling and passes after it. Further tests preserve a
platform-held monitor and a worker's exception region across an interleaved
callback, checking distinct jump buffers and cleanup on completion.

The 148 captured key events were replayed in isolated fresh and copied-save
runs. Original timing did not reproduce the live hang in either build. With
the intervals scaled to 90%, the original build hangs at event 78 in the
actual `keyNotify` body at 0x8e0d and reaches a ten-million-instruction
diagnostic ceiling. The fixed build completes all 148 events and 100 further
ticks: 3,737 ticks, 37.001 guest seconds, 275 flushes, no runtime error, and a
visible gameplay frame. Evidence: `baseline-timing-90.log`,
`fixed-timing-90.log` and `fixed-timing-90.png` under the ignored incident
directory.

`make test`, `make test-debug`, `go test -race ./internal/...`, `go vet ./...`
and `git diff --check` pass. The first whole-repository run encountered mixed
packages in diagnostic overlay copies; those local files were renamed to
non-Go inputs and the gates rerun successfully. A replacement debug server
binary was built separately; the user's live server was not replaced or
restarted, because a restart loses unsaved runtime progress and needs their
confirmation.

This fixes the evidenced input/worker wait. It adds no out-of-band report
export for arbitrary blocked guest callbacks and does not claim that all later
gameplay has been exercised.

<a id="lgt-key-callback-hang-2026-09-23-user-acceptance"></a>

### User acceptance

After restarting the corrected debug server, the user reported that the other
cases passed and only case 7's download prompt remained, so the key-callback
hang is accepted on the user's gameplay route. The separate stale
download-state recovery is recorded in the
[ten-case QA report](#lgt-qa-2026-09-21-existing-download-state-recovery--2026-09-23).

<a id="lgt-new-archives-2026-10-19"></a>

## Newly added archives — 2026-10-19

Six LGT and two KTF archives were added to the local corpus and run from empty
debug saves with periodic confirm presses, then driven through each decision
point with `-serve`. Five LGT titles and both KTF titles reached play or their
main menus without code changes; this section records the two that did not and
the authentication prompts the others show.

<a id="lgt-new-archives-2026-10-19-java-surface"></a>

### A Java title's first four missing members

One AOT Java title stopped in `startApp` on `Clip.getVolume()I` (vtable slot
11), then, once each was answered, on `Jlet.getEventQueue()`, on Vector's
baked slot 10 in its first worker, and on `Card.getDisplay()` in its first
`keyNotify`. The answers:

- `Clip.getVolume` returns the clip's own level, the value `setVolume` and
  `MC_mdaClipGetVolume` already share (100 until set).
- `Jlet.getEventQueue` returns one `EventQueue` object for the life of the
  title. This title only takes the queue; `postEvent`, which its class list also
  names, was not reached and remains unimplemented.
- Vector slot 10 is the class's first declared method, `copyInto(Object[])`.
  The call site asks slot 15 (`size`) for a length, allocates a `String[]` that
  long, passes it and keeps it. The elements go to the front of the array; a
  null array throws `NullPointerException` and a short one
  `ArrayIndexOutOfBoundsException`, both as guest throws. A primitive array,
  which the declared parameter keeps a verified caller from passing, stops the
  title with a runtime error instead of taking the references as numbers; an
  `int[]` is as wide as an `Object[]`, so the check reads the array's type
  rather than its width. `System.arraycopy` shares that view of an array and
  refuses a copy between references and numbers the same way.
- `Card.getDisplay` returns the default display singleton.

With these the title reaches its notices, title, menu, slot selection, opening
story and first in-game tutorial screens with no runtime error: 4,200 ticks of
confirm presses, and an 11,200-tick run that switches to directions, confirms
and the right soft key after the opening. A restart on that save reaches the
slot menu. Members its class list names that no run reached stay unimplemented and
are reported by name if called: `EventQueue.postEvent`, `Component.repaint`,
`ContainerComponent.repaint`, `ShellComponent.getCard`, `grabKey`,
`setGrabKeyListener`, `File.seek`, the `Hashtable(int)`, `Integer(int)`,
`IOException()` and `NoSuchElementException()` constructors, and the `Socket`
stream methods. A diagnostic build that stubbed them recorded no call in either
run. Each new answer has a unit test.

<a id="lgt-new-archives-2026-10-19-prompts"></a>

### Authentication and service prompts

- A title whose server certificate was missing could not leave its title
  screen; its exchange is the second revision recorded in
  [certificate message](#lgt-new-archives-2026-10-19-certificate-message).
- Two titles try a network authentication at start, fail the dial and show
  their own failure notice, then continue to the title and play. That is the
  documented
  [refusal path the titles already handle](#implementation-network-authentication-is-a-path-the-titles-already-handle);
  one of them writes its own registered flag after the failure and skips the
  prompt on the next start. In that title, choosing No at the first-run
  authentication question loops back to it; choosing Yes, then declining SMS
  reception and the remote-save lookup, reaches the main menu.
- Two titles ask whether to look up a remote save. Declining, then confirming
  the follow-up warning, continues; accepting fails the connection and offers
  to continue without the remote save.
- A KTF title asks to fetch a missing certificate. Declining reaches the menu,
  style selection and play.

<a id="lgt-new-archives-2026-10-19-certificate-message"></a>

### Certificate message: a second revision

The title (native module SHA-256
`c3abb052c4eda11bba233e1bf21641fe04c5fb276b8fda9af4f581a1d7b1be8c`) reached its
title and then stopped on its missing-certificate prompt: declining returned to
the title, and accepting failed the dial and returned there too. Its startup
reader checks `cert.c2s`; with no file it clears a 76-byte record and asks to
connect. With a file it reads exactly 76 bytes, decrypts them with a key the
module carries, and compares the subscriber number at offset 1 (failure 5001)
and two client strings at offsets 13 and 33 (5002, 5003) against the values its
own client reports. The writer builds the same record from those values and
saves it after the server's final reply. Nothing in the archive packages a
certificate, so a session cannot supply one without the client's own values;
answering the exchange lets the guest build and save its own record.

The exchange is the one `lgt.certificate_message` already answers, with the
same frame layout. A diagnostic build that accepted the dial for this module
recorded the guest's own requests (connect `0x65, 0xfc4273d3, 0x223b, 0x1e1c5`,
dial callback `0x1ddf5`):

- first request, 73 bytes: the same header, client field `LGT_WIPIC_1.0.0`
  where the first revision sends `V.1.0.1`, then the session number;
- second request, 79 bytes: the same header with code word `04 29` where the
  first revision sends `04 00`, the handset model, a zero byte, and the product
  field `0003060B`, which the first revision leaves empty.

A second run with another subscriber number changed only the number in the
first request. The client field, code word, product field, callbacks and
endpoint are therefore per-revision constants, kept in
`certificateMessageRevisions` beside the first revision's. That table is keyed
by the module digest, not the descriptor's application identifier: the digest
names the code exactly, and an identifier would add a second key that could
disagree with it or differ only in letter case. The registry entry
`lgt-certificate-message-002` names the same digest, and a test requires every
revision in the table to be registered. The replies are unchanged: the
five-byte acknowledgement and the local informational message.

Route, debug build, fresh save: two notices, the remote-save question
(declining it, then confirming the follow-up), the title, the certificate
prompt (Yes). The guest shows `[인증 성공]` with the local message, writes its
76-byte `cert.c2s`, and asks its own customer-information consent; declining it
returns to the title. A new process on a copy of that save shows no remote-save
or certificate question, reaches the main menu at tick 1,908 and the game
selection screen after `게임시작`. `TestCertificateMessageRevisionRequests` pins
both revisions' recorded requests and socket contracts as fixed bytes, both
revisions run the fragment and changed-byte checks, and an unregistered module
is refused. Remote saves, ranking and purchases remain unavailable.

<a id="lgt-text-input-2026-10-20"></a>

## A name field the key rule refused — 2026-10-20

A Clet title from the 2026-10-19 additions ran, but its character-name screen
(new game, slot, class, then the name) showed 입력 불가능 when the text input
dialog was opened there. The player had pressed only the confirm key on the way.

**What the title does with the input method.** A run with `-trace-live " im"`
and the same presses, then a probe through `Session.TextInput`:

- While starting, before any key: `MC_imGetSupportedModeCount`,
  `MC_imGetSupportedModes`, `MC_imSetCurrentMode(2)` and ten
  `MC_imHandleInput(0x9d)` flushes, all with the completion buffer the name
  screen later uses.
- Notices, title, menu, slot and class selection: no input-method call.
- The name screen: every key press, `0`, `2`, `5` and `*` included, reaches
  `MC_imHandleInput` from one call site in a wrapper that hands the automaton
  the key. On arrival, with no key pressed there, nothing calls it. The key
  that opens the screen is the class screen's confirm.
- Every frame of the name screen copies the completion buffer into the string
  it draws (`strcpy`, `strcat`, `strlen`); no other screen reads the buffer.

The availability rule then in force ignored calls before the first key and
closed input on any key the Clet handled without the input method. Input was
therefore closed on arrival and opened only by a key pressed on the name screen
itself. The carrier route was never the problem: after any key there, a commit
of a two-character name appeared in the field, and the title accepted it and
went on into its opening.

**The decision.** Reading the completion buffer back would have been evidence
of the widget at work, and the probe showed it opening input on arrival. It was
not kept: it would have needed the buffer address in quick-save records, and a
widget that copies its buffer with its own loop would still be refused. The
user's call was the simpler one: a field the player cannot type into is the
failure that matters, an offer on a screen without a field costs only a commit
that reports failure, and so availability should err toward offering, on every
platform. LGT C input is offered again from the first input-method call on,
start-up configuration included. A carrier that reaches no automaton is followed
by its release, so the stray key is a whole tap rather than a key held down, and
the offer stays. A load of a record written while input followed the keys
offers input to a title whose record shows input-method calls
(`TestLGTCInputOfferSurvivesARecordThatHadItClosed`).

Result on the same route, from an empty debug save: the snapshot on arrival is
offered, a two-character name lands in the field, and the confirm key goes on
to the opening. Four `*` presses on the name screen, each followed by a
one-character commit, append all four characters: the wrapper's mode changes
do not keep the carrier from the automaton in this title. Its disassembly shows
modes in which the wrapper drops `0` or sends keys through its own table
instead; those were not reached.

**LGT Java fields at the same time.** The Java route required a focused field.
It now takes the focused field when it can, and otherwise the only text
component under a shown shell, or, with none there, the only one that never
joined a shell. Repeated show and focus notifications and input-mode selections
no longer make a pending edit stale; real visibility, focus, parent and value
changes still do (`TestTextInputChoosesAnUnfocusedFieldOnlyWhenItIsTheOnlyOne`,
`TestTextInputCommitSurvivesRepeatedNotificationsAndModeSelection`). A static
scan finds text component class names in ten local LGT titles (twelve
archives); no route to a Java name field was run.

**KTF and SKT at the same time.** KTF asks its six routes twice: first under
the rules they had, with a candidate that cannot take the edit handing the
question on instead of ending it, then, only when that finds nothing, under
relaxed rules — any shown shell in a focused field's chain, the newest shell
still shown, a drawn field without focus that is the only one read, the vendor
form's current contents, a title's own field until its listener is removed, and
an unknown C caller on any card once the title has used the input method. SKT's
fallbacks are recorded in [its history](skt.md#skt-text-input-2026-10-20). Both
rest on unit tests (the KTF ones include `TestTextInputKeepsTheFirstRulesAheadOfTheRelaxedOnes`,
`TestTextInputFallsThroughAStaleFocus` and, as the review follow-ups below left
it, `TestCInputUnreachedCarrierIsReleasedAndWithdrawsTheOffer`); no KTF or SKT
title was driven to a name screen for this change.

<a id="lgt-text-input-review-2026-10-20"></a>

## Text input review follow-ups — 2026-10-20

A review of the change above, on all three platforms, found twelve faults.
None of the fixes reaches anything but Host text input: they change which
field a snapshot names, what a commit's carrier does, and what a failed commit
leaves behind.

**What a failed carrier leaves.** A commit through a carrier key on a screen
whose title takes no text still reaches the game as one press and its release.
Before, the offer stood, so every retry pressed the key again. Now the failure
is evidence and the offer waits for new evidence:

- KTF and LGT WIPI-C: after a carrier that reached no input method, the
  unknown-caller offer waits for the title's next input-method call or the
  player's next press, and on KTF the editor is no longer active, as it was
  before the relaxed rules (`TestCInputUnreachedCarrierIsReleasedAndWithdrawsTheOffer`,
  `TestCInputUnknownEditorIsOfferedOnANewCard`,
  `TestLGTCInputIsOfferedOnceTheTitleUsesTheInputMethod`). A load offers input
  again, as the next press would.
- A KTF title's own `InputMethodHandler`: a carrier the title keeps from the
  offered field ends the offer, since a field still on the screen is handed
  every key; handing it a key again offers it again. A carrier the title hands
  to another of its handlers types nothing there and makes that handler the
  open field, and a carrier that reaches an LWC text component types nothing
  into it either (`TestAnAbandonedTitleFieldGivesWayAtItsFirstCommit`). Before,
  an abandoned handler with its listener still set was offered, and failed, at
  every commit, ahead of every other relaxed route, and a stray `0` was typed
  into the handler or component the title used instead.

**A commit that stopped after one character.** On KTF, the first character of
a commit into the offered field reopened that field, and reopening counted as a
change that spent the commit: of `홍길동`, only `홍` arrived. Reopening the
field already offered on the same card no longer spends anything, and a
checkpoint written before the offered record existed now keeps offering its
open field after a press closes it, as a running session does
(`TestHostTextFillsAnOfferedFieldThatWasNotOpen`,
`TestTheOfferedFieldFallsBackToTheOpenOne`).

**Fields offered on evidence that had gone.**

- KTF drawn fields: the relaxed set of fields the last text-reading paint read
  is tied to the card that painted it and offered only while that card is on
  top; it is dropped when that card leaves the display
  (`TestTextInputOffersAFocusedFieldTheCardDraws`,
  `TestTextInputForgetsTheTextOfARemovedCard`).
- LGT fields in no shell: a field is one the title draws only when the pushed
  card read its text during its last paint that read any, so a helper field
  never drawn, or a dialog's previous field not yet collected, is not offered
  or counted (`TestTextInputOffersAFieldInNoShellOnlyWhenThePaintReadsIt`).
  The record is not part of a checkpoint; the next paint fills it.
- LGT focus: a focused field on the screen that refuses a Host edit, because a
  composition listener is set, keeps a field beside it from being offered, as
  on KTF and SKT; a focus left behind a hidden shell still hands the question
  on (`TestTextInputDoesNotPassAFocusedFieldOverForAnother`).
- SKT: see [the SKT record](skt.md#skt-text-input-2026-10-20).

**An LGT record from before the change.** A load derived input-method use from
the recorded calls only. It now also reads the mode table a title that listed
its modes built, and a selected mode other than the first, as the KTF side
already did (`TestLGTCInputOfferSurvivesARecordThatHadItClosed`).

**Cost.** A KTF commit checks at every character that a snapshot would still
name the same editor, and the relaxed shell rule walks up to 1024 components.
With such a shell up during a 64-character C commit, the commit took 24 ms; a
visited set kept between walks and a check that no longer repeats the one just
made bring it to 11 ms. The vendor form's field scan is now one function for
the strict and the relaxed rule.

**Nothing outside text input moved.** Every local archive, 800 of them, ran 500
ticks with `run` on a debug build of the change's base and of these fixes, each
with a throwaway save root. 777 ended on the same screen digest. The other 23
are all SKT titles, which run on the wall clock: run again, the base alone gave
each of them more than one digest or gave the digest the fixes gave; the one
whose base digest held across three runs gave three digests in eight more runs
of the base, and two of those in eight runs of the fixes. No title was driven
to a name screen for these fixes.

<a id="lgt-earlier-java-package-2026-10-21"></a>

## A package of the earlier Java platform — 2026-10-21

A newly added archive was refused at start with "the archive is not a KTF, LGT
or SKT game". It is whole — its JAR's 155 entries read and inflate — and it is
a package of the carrier's Java platform from before WIPI:

- `DESC.jad` and `MANIFEST.MF` declare a MIDlet for `MIDP-1.0` and `CLDC-1.0`,
  with the carrier's own keys beside the standard ones (`MIDletX-LCD-Size:
  176,200`, `MIDletX-No-Command`, `MIDletX-Expire-Date`), and the JAR's URL
  names the carrier's Java download path.
- The JAR holds class files and the title's resources, and its classes link
  against `javax/microedition/{lcdui,io,rms,midlet}` and the carrier's
  `mmpp/microedition/lcdui/GraphicsX`, `mmpp/media/…` and `mmpp/phone/…`.
- `<n>.idx` beside `<n>.jar` indexes the JAR's entries by name, offset and
  size, and the `.db` files beside them are the title's record stores.

There is no `app_info`, no `binary.mod` and nothing compiled, so neither the
LGT loader nor the SKT one (whose classes link against `com/skt/m/…`) applies,
and detection answers `no-marker`. Running it would take a MIDP 1.0 platform
with the carrier's `mmpp` extension classes and its package layout; nothing of
that exists here, and building it is not planned for now (decided 2026-10-21).

<a id="lgt-slot-corrections-2026-10-22"></a>

## Image slots, C library names and the storage contract — 2026-10-22

A comparison of the slot table with the v2.x specification's function order
(2026-10-21) found one slot served as the wrong function, five C library slots
served without a name, and two file slots and nineteen Java storage members
missing. The same day the three platforms' storage was put under one contract,
[WIPI storage](../wipi-storage.md), and the file slots and Java members were
built to it.

<a id="lgt-image-slots-2026-10-22"></a>

### The image slots follow the v2.x order

Counted from `MC_grpGetImageProperty` at `0xc8`, the v2.x order puts the image
calls at `0xe9` `MC_grpCreateImage`, `0xea` `MC_grpCreateSubImage`, `0xeb`
`MC_grpDestroyImage`, `0xec` `MC_grpDecodeNextImage` and `0xed`
`MC_grpEncodeImage`. The table had the destroy at `0xea` and the next-frame call
at `0xeb` from its first commit (2026-08-15), with no call behind either: the
v1.2.1 list, which has no `MC_grpCreateSubImage`, moved up by one. The create
and the encoder were confirmed by calls; the two "unnamed gaps" the block was
read as having are the two functions v2.x adds (corrected
[above](#implementation-0xf0-is-mc_grpfillpolygon-and-the-block-has-two-unnamed-slots-in-it)).

From 1,500-tick runs of the 132 local titles (2026-10-21):

- 27 titles call `0xeb`, 5,228 times in all, mostly once after each `0xe9`.
  Three call sites in three titles, disassembled, are release code:
  `if (obj->img) { 0xeb(obj->img); obj->img = 0; } return 1;`,
  `if (*pp) 0xeb(*pp);`, and a destructor that frees a buffer, calls `0xeb` and
  then clears the structure with `memset`. Served as `MC_grpDecodeNextImage`,
  every one of them was answered "no further frame" and freed nothing.
- A Clet has no collector (`collectForAllocation` runs for a Java title only),
  so the images it let go stayed in the 16 MiB surface region.
- An A/B with `0xeb` as `MC_grpDestroyImage` and `0xec` as
  `MC_grpDecodeNextImage`, seven titles for 3,000 ticks each: the frame digests
  were identical at all 12 checkpoints; surface use after 3,000 ticks was
  1.17–2.57 MB before and 0.31–1.13 MB after, and live framebuffers 380–2,094
  before and 2–744 after.
- No title calls `0xea` or `0xec` in those runs. Both are placed by the order
  between the confirmed slots.

`MC_grpCreateSubImage(srcImage, startx, starty, width, height)`, with the height
on the stack, answers a new image of `width`×`height` holding the source's
pixels from `(startx, starty)`, or NULL. The decisions:

- **The region has to lie inside the source and hold at least one pixel**, or
  the answer is NULL, the specification's one failure answer; an unknown source
  is NULL too, and a refusal allocates nothing. Clipping would answer an image
  of a size the caller did not ask for, which a caller walking it through its
  framebuffer pointer with its own width would overrun. `MC_grpEncodeImage`
  clips, and can, because its answer carries its size in the bitmap header.
- The source is read back from guest memory first, since a Clet writes images
  through their pointers, and the copy is an image of its own: destroying the
  source leaves it.
- **The mask comes with the pixels**, as the specification says. A colour-keyed
  source gives a sub image with the same key and a mask read off the copied
  pixels, which is where the key is after a guest has moved pixels around; a
  source with only a positional mask gives the matching part of it; a source
  with neither gives an image with neither.
- A surface region too full for the copy stops the run, as it does for
  `MC_grpCreateImage`: it is this platform's shortfall, which a NULL would pass
  off as the title's.

After the change (2026-10-22), a title that calls `0xe9` and `0xeb` 71 times
each in 1,500 ticks has every `0xeb` served as a destroy, and its final frame,
step count and guest time are identical to the previous build's; a title that
makes no image call is identical as well.

<a id="lgt-stdlib-names-2026-10-22"></a>

### Every served C library slot has a name

`importImplemented` reads a C library slot that `stdlibSlotNames` does not name
as one `handleStdlibSVC` does not serve. Five served slots had no name: `setjmp`
(`0x32`, the jump half of a Java try region), `vsprintf` (`0x3f9`), `rand`
(`0x403`), `srand` (`0x404`) and `malloc` (`0x426`). A trace printed them as
`unnamed`, and `apiscan` reported one title as "gap 1 (stdlib 0x32)" although
all 675 of its `setjmp` calls in 600 ticks were served. The five are named now,
and a test runs slots `0` to `0x7ff` through the handler and holds the set it
serves and the set the table names equal.

<a id="lgt-storage-contract-2026-10-22"></a>

### The storage contract

KTF, LGT and SKT now answer a title's files, directories and databases the
same way, as [WIPI storage](../wipi-storage.md) gives it; a platform that
answers otherwise has a defect. What changed here, and why:

- **New C slots and Java members.** `MC_fsSetMode` (`0x19d`) and
  `MC_fsGetCounts` (`0x19e`) are placed by the file block's order, between
  `MC_fsAvailable` and `MC_fsTell`, both of which titles call; no 2026-10-21
  run reached either (a call stopped a run then, and none stopped there). The
  seventeen `FileSystem` members, `File.seek`, `File.openDataOutputStream` and
  `DataBase.sortRecord` are served. `sortRecord` calls the title's own
  `DataFilter` and `DataComparator` back the way a `Runnable`'s `run` is
  reached: by the member its class record names, or through the slot its
  interface table gives. One local title's class list names `File.seek`.
- **Names.** A name is read one way by every file and database call: a
  backslash separates segments as a slash does (as this platform always read
  it), and leading separators, empty and `.` segments are dropped. The
  longest name is 255 bytes of that path — a C name's own bytes, a Java
  `String`'s UTF-8 bytes — on every call (`M_E_LONGNAME`, `IOException`); it
  used to be counted on the name as spelled, and a spelling of more than 509
  bytes had no save key at all. A name no file can have — a `..` segment,
  the platform's own lists, the title's own directory where a file is meant —
  is `M_E_BADFILENAME` or `IOException` there too, where an open of one used
  to hand out a handle whose writes reached nothing. The archive is asked for
  the path too: its own lookup drops one leading slash, so
  `//data/a.txt` and `\data\a.txt` missed a packaged file that `data/a.txt`
  found. A null name is `NullPointerException` to a `File` or `FileSystem`
  call and `DataBaseException` to a `DataBase` call.
- **Directories.** A directory is the root, a name `mkdir` recorded, or a name
  with a file or a made directory below it. The store makes no directory
  entries, so the names `mkdir` makes are kept in a list beside the saves,
  `fs/.directories`, the platform's own like `.removed` and `.created`; old
  saves need nothing, since they made none. `MC_fsMkDir` and `MC_fsRmDir`,
  which used to succeed whatever they were asked, now answer `M_E_EXIST`,
  `M_E_NOTEMPTY`, `M_E_NOENT` and, for the root, `M_E_BADFILENAME`;
  `MC_fsIsExist` and `MC_fsFileAttribute` answer for a directory
  (`MC_FILE_IS_DIR`, 0x01, the v2.0 HAL's value); `MC_fsList` ends an empty
  list with two NULs where it wrote one; `MC_fsRemove` answers `M_E_NOENT` for
  a name that is not a file. Java `exists`, `isDirectory`, `list`, `mkdir`,
  `rmdir` and `remove` follow the same table, `list` of a name that is not a
  directory and `remove(name)` of one that is not a file throwing the
  specification's `IOException`, and `remove(name, flag)` returning. A make
  that always succeeds would tell a title that reads success as its first run
  that every run is the first.
- **Refused removals.** A removal the store refuses no longer happens: the
  removal list is given to the store before the session lets the file go,
  and a refusal leaves the file, with `MC_fsRemove` answering `M_E_ERROR`,
  either `remove` throwing `IOException` and `deleteDataBase` throwing
  `DataBaseException`. The removal used to happen in the session and wait to
  be stored, so a title was told it had removed a file the next session could
  still find. A rename, which has stored the file under its new name by then,
  still leaves a refused list waiting.
- **Space.** The room is 1 MiB, and the room left is that less what the title
  has stored: for each file it wrote, its size less the size of its archive's
  file of the same name, and for each database the bytes of its records. A
  file written shorter than the archive's adds nothing rather than taking the
  difference off another. The archive's paths are asked about once a session
  and again only after a write of theirs. Nothing is refused for room. The
  bound on untrusted input is 4 MiB for one file, down from 16 MiB, and for one
  database's records; a write past it is `M_E_NOSPACE`, an `IOException` from a
  `File` or a stream on one (where a stream write used to stop the run and
  `File.write` to answer -1), or a `DataBaseException`.
- **Databases.** `DataBaseRecordException` extends `Exception`, not
  `DataBaseException`. The record size is no longer enforced (a longer record
  used to be refused); `closeDataBase` leaves the object usable (it used to
  stop it); `getAccessMode` answers `PRIVATE_ACCESS` for a database that exists
  and throws for one that does not (it answered 3 for any name);
  `listDataBases` is null when there is none; `getDataBaseSize` sums the
  records' bytes (it answered the container's size); `getLastModified` is the
  last write in the session or the session's first open;
  `selectRecord(int, byte[], int)` throws `IllegalArgumentException` for a
  buffer the record does not fit; `openDataBase` with `create` and a record
  size of 0 or less throws `IllegalArgumentException`, whether or not the
  database exists, and a container an earlier build wrote without a record
  size takes the size of the open that finds it. A write through an object
  opened before the database was deleted makes it again and puts its name
  back on the index.
- **Database names** are read as file names are, so `/save`, `\save` and
  `save` are one database and `dir/scores` is a database in `dir` (a `/` or
  `\` used to be refused, and names were at most 128 bytes). The name is the
  path from then on: what `getDataBaseName` and `listDataBases` answer. The
  index of names is read a line at a time exactly as written; it used to trim
  each line, which left a database whose name began or ended with a space
  out of `listDataBases`. Names in an index an earlier build wrote are single
  segments, so they read the same; one that is no name now (`.`, `..`) is
  left out of the listing.
- **Containers are no files.** A database here is still one container file
  under `fs/`, `<name>.db`, where the other two platforms keep databases apart
  from the files, and the file calls used to see it as a file of the title's.
  Now the containers of the databases the index names are filtered out of
  what the file calls see, with no change to the store: no listing, count,
  existence, kind or creation-time answer has one, a resource read finds
  none, and a database in a directory does not make the directory one. An
  open that would create a file at a container's path, and a rename onto
  one, are refused as a name no file can be kept under, because the file
  would be the container; an open to read, a removal, a rename from it and
  an `rmdir` answer as for a name that is not a file. A container the index
  does not name (one an earlier build left without its name) is still a file
  to them, and a packaged file at a container's path is hidden while the
  database is there.
- **Databases leave the files.** The filter above left one difference: a
  title here could not keep a file `X.db` beside a database `X`, which KTF and
  SKT allow. A database now has a scope of its own, `jdb/<name>.db` with the
  index of names at `jdb/.index`, and a store an earlier build wrote moves
  there the first time a session reads its databases: the containers and the
  index go to the store in one batch, and only once it has taken them are the
  old paths put on the removal list. A store that refuses the batch keeps its
  layout, and the session reads and writes the databases where they were,
  with the filter hiding them, until a later session can move them; a second
  session finds the move made and writes nothing. A quick save taken before
  the move holds its databases by name and loads after it from the scope.
  `TestDatabasesMoveOutOfTheFilesOnce`, `TestARefusedMoveKeepsTheEarlierLayout`,
  `TestAQuickSaveFromBeforeTheMoveLoadsAfterIt` and
  `TestStorageContractDatabasesAreNoFiles` hold it; the checkpoint test that
  refused a pending file and database on one key now keeps both, since they
  are no longer one key. Over the corpus at 500 ticks from empty saves, every
  LGT title ends on the same frame as before; the eight that keep databases
  hold them under `jdb/` with each container and the index byte for byte what
  the earlier build wrote under `fs/`, and no longer write `fs/.created` for
  them.
- **A comparator's exception is the title's.** A `sortRecord` filter or
  comparator that throws without catching used to stop the run, because the
  frame that catches it is outside the call this platform makes into the
  title. The uncaught throw now carries its object out of that call, and
  `sortRecord` throws it again at the depth of the title's own call, where the
  title's try region takes it, as on KTF and SKT
  (`TestStorageContractDatabaseSortRecordThrowsToTheTitle`).
