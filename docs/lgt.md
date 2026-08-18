# The LGT platform

LGT (LG Telecom) shipped its own WIPI implementation. Two things make it a
different platform from KTF, and everything else follows from them:

| | KTF | LGT |
|---|---|---|
| Binary format | raw ARM image (`client.bin`) | **ELF** (`binary.mod`) |
| Function binding | direct callback pointers | **import table lookup** |
| C standard library | linked into the game | **provided by the platform** |

The ARM core, the save boundary, the glyph font and the archive handling are
shared with the rest of the runtime; only the loader and the platform surface
are new.

Every non-Java title in the local set boots, draws, plays sound, takes input,
and has been driven from its title screen through its menus into the world —
including the ones that authenticate over a network that is not there, because
a refused connection is a path they already handle. What follows is written
from what those runs showed, and several of the contracts below are
corrections that only a real title could have found.

## What an LGT title is

A zip holding `app_info` — a flat `key=value` descriptor naming the AID, PID
and MClass — beside the game's JAR. The JAR holds `binary.mod` (the ELF) and
the game's resources.

The JAR is normally named after the AID, but repacked archives keep the
original JAR next to an edited `app_info`, so **the JAR that is actually there
wins** when the AID-named one is missing.

`SaveOwner` is the PID, with the AID as a fallback, because the PID is what the
handset's own save layout is keyed by.

**PIDs are not reliably unique.** Two titles in the local set carry the same
PID with different AIDs, so they resolve to the same save directory. Whether
that is right depends on something no archive here answers: if the handset
collided too, sharing the store is what it did; if a repack wrote the wrong
`app_info`, the identity is simply wrong. Nothing has been changed on a guess —
the PID still wins — but a title reading another's save is the shape this would
take, and the AID is the discriminator if it ever has to be split.

## Loading

`ParseModule` reads ELF32 little-endian ARM executables and nothing else:
a wrong class, endianness, type or machine fails at the header rather than as
a wild jump later. Sections with a nonzero address are loaded at that address;
`SHT_NOBITS` (`.bss`) occupies space without file bytes.

**One mapping spans the module** rather than one per section. ELF sections are
page-adjacent, and per-section mappings would leave the gaps between them
unmapped — which a module that addresses its own padding would then fault on.

There is no ELF library here. The project takes no new dependencies, and the
parts of ELF that would justify one are the parts this loader never touches.

## Startup

1. The platform hands the entry point two blocks. The first is scratch the
   module fills in with a pointer to its init struct; the second carries two
   callbacks: **get import table** and **get import function**.

   **The entry point is entered in Thumb**, and the ELF does not say so: every
   archive here has an even `e_entry` pointing at Thumb code. Taking that low
   bit at face value runs the module in ARM, where the halfword pairs decode as
   conditional instructions that mostly get skipped — so it does not fault at
   the entry but a few instructions later, on whichever one the flags let
   through, reading an address that was never a pointer.
2. The module resolves every platform function it needs through those, at
   startup, before it does anything else.
3. The platform reads `param1->ptr_init_struct`, whose second word is the
   module's initializer, and calls it.
4. The initializer calls `MC_cletRegister` with a table of six entry points:
   start, pause, resume, destroy, paint, handleEvent. **The runtime calls into
   the game only through those.**

`Session` drives the result: `Tick` advances the guest clock, fires timers,
delivers queued events, and asks for a paint.

## The import tables

| Table | Contents |
|---|---|
| `0x1fb` | WIPI C functions |
| `0x1` | C standard library |
| `0x64` | Java interface — resolves, but no Java app runs; see below |
| `0x1f8` | LGT's own. Contents unknown; see below |

An unknown table is an **error**, not a null pointer: a module handed zero
branches to zero and the failure then surfaces far from its cause. A *known
table with an unimplemented slot* still gets a stub, because a module resolves
everything it might use at startup and refusing there would stop a game over a
function it never calls; reaching the slot is what reports the gap.

`0x1f8` is where that distinction earns its keep. Every non-Java archive here
resolves slot `0x16` as the first thing its initializer does, and refusing the
table stopped all of them before they had registered anything — nine titles
dead at a call none of them had made yet. Nothing names the table: not the
specification, not the original runtime's symbols, not the reference
implementation, which knows the same two slots and calls them unknown.

Slot `0x16` takes four arguments and every caller discards its result, so
there is no return value to get wrong. The calls pass `(0, 0x64, 240, 0)` and
`(0, 0x65, 320, 0)` — small keys against this platform's LCD width and height,
which reads like the module declaring the screen it was built for. **Nothing
acts on that reading.** The values already match the LCD, some modules make a
third call the reading does not explain, and resizing the screen on a guess
would break every title if the guess were wrong. Accepting and ignoring is what
is known to be right; `internal/platform/lgt/oem.go` records the evidence.

### The imports a module resolves are its link map

Nothing in an LGT archive lists what a title links against. The ELF carries no
dynamic symbols, a platform function is a pair of numbers rather than a name,
and the only place those numbers appear is the resolution calls the module
makes while it starts — every one of them, for everything it might ever call,
before it runs any of its own code.

So the platform keeps them. Each `(category, slot)` a module resolves is
recorded once (`internal/platform/lgt/imports.go`), and `ResolvedImports`
reports them with the platform's own name for each and whether reaching one
would be serviced at all. `internal/tools/apiscan` starts every archive in a
directory and prints the unserviced ones, ranked by how many titles resolve
each. It is a boot per archive rather than a read, which is what the other two
platforms' scans are; there is nothing cheaper here that is also exact. (The
stub-record scan under "Reading a module without running it" recovers the same
list statically, but it needs a disassembler this repository does not carry.)

What the local corpus says: **the 25 Clet titles resolve nothing this platform
does not implement** — the C surface is complete for everything they link
against, which is a different and stronger statement than "they run". Every gap
is on the Java side and belongs to the four Java titles: three auxiliary Java
tables (`0x1fc`, `0x1ff`, `0x201`, index 3 of each), the OEM table's Java entry
`0x17`, and C library slot `0x32`, which sits far below the C library's own
base and is resolved by no Clet.

The scan also found a naming hole rather than a gap: `0x424`, the slot that
runs a function, is serviced but was missing from `stdlibSlotNames`, so every
trace line for it read `unnamed` and the scan counted it as unimplemented. The
table is the platform's record of what it services, and a hole in it is a hole
in both readings.

## The WIPI C slots

They are a flat index in blocks of 100 (`0x64`). Four blocks are identified,
and each one was confirmed by the functions inside it rather than by the
arithmetic: `0x64` kernel, `0xc8` graphics, `0x190` filesystem, `0x258`
network. The C library is a table of its own (`0x1`) with its base at `0x3e8`.

**Within a block the slots follow the specification's own function order**, and
that has been checked rather than assumed: the kernel block's `0x64` is its
first function, `0x6a` its seventh, and `0x75` through `0x81` its eighteenth
through thirtieth — fourteen consecutive slots landing exactly where the
specification lists them. The filesystem block agrees the same way (open, read,
write, close, seek, attribute, remove … `0x1a0` is the seventeenth,
`MC_fsIsExist`), and so does the network block, whose imported set — connect,
close, socket connect/write/read/close, the two callback setters — is exactly a
socket client's needs at exactly the specification's offsets. That is enough to
read an unidentified slot's number as a name **inside an identified block**.

**It is not enough to identify a block.** The blocks past the filesystem do not
follow the specification's section order at all: media sits at `0x4b0`, where
the sections would put the C library. The arithmetic that would make block three the record
database is not evidence either, and it was wrong: block three is the
input-method block, and no title here imports a database call at all. See
"Character input" below for how that was settled, and
`internal/platform/lgt/wipic_unknown.go`, which is where a slot answered
without being understood is recorded and is currently empty.

**Everything below `0x64` is LGT's own OEM block**, and that is where the
framebuffer accessors live: `0x32` pointer, `0x33` width, `0x34` height,
`0x35` bytes-per-line, `0x36` bpp. Some titles render nearly everything
through them — sprites, tilemaps, their own Korean bitmap font — so these five
matter far more than their call count suggests.

`0x36` is the odd one: **it takes no argument**, because it reports the depth
of the LCD rather than of a framebuffer.

Three of these had contracts that looked right until the call that depended on
them arrived, and each one ended as a null the game then walked into:

- **`0x80` / `0x81` are a pair.** `MC_knlGetResourceID` turns a name into an id
  and reports the size; `MC_knlGetResource` reads *that id*. Answering the size
  from the first call satisfies a title that only wants the size, and then hands
  the second call a length where it expects a name. The id is a guest pointer to
  a copy of the name, which is the handle KTF hands out too.
- **One name answers one id**, for the life of the client. The id is the
  resource's *identity* to a title, not a scratch handle it is finished with
  when the read returns: one title reads its whole resource list at boot —
  a packaged file of names, and a second of sizes beside it — and builds an
  `{id, size}` table from them, then loads a resource by asking for its id a
  second time and searching that table for the value. Handing out a fresh copy
  of the name per call makes every one of those searches miss. What a miss
  costs is the reason this is here rather than in a note about allocation: the
  lookup leaves the caller's size at the zero it had initialised, so the title
  allocates nothing for the data, reads its record count out of whatever heap
  follows the empty buffer, sizes a table from that number, and walks off the
  end of it. Its loop counter is a `signed char`, so a count above 127 wraps to
  -128 and the writes land *below* the heap. The title died there, several
  thousand platform calls past the call that caused it, and the ring buffer at
  the fault showed only a loop allocating one byte at a time for names that
  were all empty.
- **`0x81` answers zero, not the byte count.** Its callers are written as
  `id = getResourceID(name, &size); buf = calloc(size); if (getResource(id, buf,
  size)) { free(buf); return 0; }` — a nonzero answer is the *failure* branch.
  Reporting the length freed the buffer that had just been filled and handed the
  caller a null it copied from a few instructions later, which is the shape three
  titles died in. The one documented failure is a buffer too small for the
  resource, `M_E_SHORTBUF`.
- **`0x76` takes one size.** `MC_knlCalloc` is "allocate and zero", not C's
  `(count, size)` array allocator. Multiplying in a second argument that is
  really the caller's stack pointer turns a 33-byte request into one no arena
  can serve.
- **`0x7e` answers.** Titles read `PHONEMODEL` and `PHONENUMBER` while deciding
  which path to take. The answers are shared with KTF in `internal/wipic`,
  because the question is about the handset rather than about which runtime is
  being asked.
- **`localtime` returns a struct.** It used to answer a null pointer, on the
  reasoning that laying out a `tm` for a specific libc was a guess no title had
  asked for. One title asks: it calls `localtime` and loads the field at offset
  4 straight off the result. The specification lists `localtime` among the
  "supported ANSI-C interface functions" without restating its interface, so
  the contract it defers to is ANSI C's — nine ints in declaration order, in
  storage the caller does not own and must not free.

### `MC_knlExit` sits at `0x68`

It was `0x6b` here, which the kernel numbering makes
`MC_knlGetParentProgramID`. A title asking who launched it would have been
answered by ending its own session, and one asking to exit would have been told
the slot does not exist. No local title reaches either, so this was corrected
from the numbering rather than from a failure.

### `MC_knlSprintk` renders a real printf, precision included

The renderer is shared with the other WIPI platform — `internal/wipic` — because
a format string means the same thing wherever the call arrived from. It accepts
`%%`, `%c`, `%s`, `%d`, `%i`, `%u`, `%o`, `%x`, `%X` and `%p`, the `-`, `0`,
`+`, space and `#` flags, a width, a precision, and the `h`/`l` length
modifiers. Width and precision may each be given as `*`, which reads an
argument word of its own.

**A conversion the subset cannot parse is emitted as written**, which is what
makes a missing one legible instead of invisible — and it is how the precision
came to be implemented. A title's dialogue is one long block of text with each
line stored as a byte count into it, so every line is drawn with `%.*s`; with
`.` unrecognised, the whole specification reached that fallback and **every
line of dialogue in the game read `%.*s`**. Two more of that title's formats
are `%s:%.*s` and `%.3s-****-%.4s`.

The precision is not a formatting detail on `%s` — it is what bounds the read.
`%.*s` prints a slice of a buffer that need not be terminated at all, so the
guest read is a counted one that stops at a terminator rather than requiring
one. Requiring one reads off the end of the slice and fails on the buffer the
title meant to print from.

### `MC_knlCurrentTime` answers 64 bits of epoch milliseconds

The specification declares it `M_Int64 MC_knlCurrentTime()`, counting
milliseconds since 1970. Both halves of that matter, and getting either one
wrong fails in a way that looks nothing like a clock problem.

**The width.** A 64-bit result comes back in `r0:r1`, low word first. Answering
in `r0` alone leaves `r1` holding whatever the caller last put there, so a
title that keeps a start time and subtracts the two as 64-bit values gets a
difference built out of a stale register. One title's loading screen does
exactly that — it draws its screen, reads the clock, and spins until the
difference passes a deadline — and with the high word unwritten the deadline is
never reached. The symptom was the instruction ceiling twenty seconds later,
reported against the timer callback the loop happened to be running in, with
the loop's own drawing calls filling the platform-call trace and nothing in it
pointing at time. Raising the ceiling twenty-fold only bought twenty times the
frames. **A slot that answers 32 bits may write `r1` freely** — `r0` through
`r3` are scratch across a call either way — so the answer is written as a pair
unconditionally.

**The epoch.** Since the value is milliseconds since 1970 rather than since the
run started, the difference between two reads is right either way, which is
what lets the mistake survive: only a title that formats the time, stores it,
or takes it modulo something shows the epoch at all. The clock's origin is a
fixed date rather than the host's, for the same reason the rest of the guest
clock is virtual — a run has to replay the same way twice for a frame diff to
mean anything.

This is the only 64-bit return among the slots this platform implements; the
kernel, filesystem and graphics blocks are `M_Int32` throughout.

### `0x97` answers the program's application id, and a title's copy check reads it

`0x97` takes the numeric program id `MC_knlGetCurProgramID` answers with and
returns a **pointer to that program's application id as a string**. Neither the
specification nor the original runtime names it, but its callers settle it
beyond doubt. One title does this during startup:

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
anti-piracy check: a copy that reached a handset any way other than through the
store answers with something else, and the title stops at a notice about
illegal downloads instead of booting. So the honest answer is the archive's own
declared AID — it is read out of the archive rather than chosen here, and it is
what a handset that downloaded this archive would say.

An archive that declares no AID, and a program id this platform is not hosting,
both get a null. That is the answer the caller already handles: it tests the
pointer before formatting it.

This was the only thing stopping one title. It now boots, and a second title
that was recorded as blocked by the same wall turned out never to have been
blocked at all — its connection-failure dialog simply needed its confirm key.

### Block nine is the utility block, and two calls named it

The blocks past the filesystem do not follow the specification's section order,
so a block is identified by what the functions inside it do. One title's
"authenticating" screen identified this one with two calls in three
instructions: it passes `0x385` a port and keeps the answer as a halfword, and
passes `0x388` a pointer to the string `"218.50.3.88"` and keeps the answer as
a word, storing both into one record beside each other. A host-to-network short
and a dotted-quad address are the second and the fifth of the specification's
six utility functions, in its own order — the same rule the identified blocks
follow — so the block is `MC_utilHtonl`, `MC_utilHtons`, `MC_utilNtohl`,
`MC_utilNtohs`, `MC_utilInetAddrInt`, `MC_utilInetAddrStr` at `0x384` through
`0x389`, and all six are implemented rather than the two that were reached.

The guest is little-endian, so both byte-order directions are the same swap.
`MC_utilInetAddrInt` answers the address the way a little-endian `inet_addr`
does — first octet in the low byte, so the word's bytes in memory are the
address in network order — and answers `-1`, which the specification documents
as its failure, for anything that is not a dotted quad. Nothing here resolves
names.

Refusing these two as network calls would have been wrong in the way that is
hard to see later: they never touch a network. The title's own dial is the
socket call it makes with what they returned, and that is where the refusal
belongs.

### Block eight is the component block, and `0x320` is not a clock

The same rule names block eight, and it corrects a reading that had stood for
a while: `0x320` was taken for the unidentified block's time call and answered
with epoch milliseconds. It is `MC_uicCreateApplicationContext`.

One title's character-naming screen says so in three consecutive calls. It
calls `0x320` with no arguments and keeps the result; it calls `0x321` with a
pointer to the string **`"TextComponent"`**; and it calls a third slot with the
two of them, in that order. `MC_uicCreateApplicationContext`,
`MC_uicGetClass` and `MC_uicCreate` are the first three functions of the
specification's component section, and `MC_UIC_TEXT_COMPONENT` is defined as
exactly that string. The arithmetic agrees — every identified block here starts
at a hundred times its number, and eight hundred is `0x320` — and so does the
neighbour: block nine, above, is the utility block at nine hundred.

Nothing rested on the old reading. No local title reaches `0x320` at all except
this one, which is not asking for the time.

**A component cannot be created here, and that is the answer rather than a
gap.** These widgets are drawn by the handset over the game's screen and typed
into with the handset's own editor; a runtime that answered a handle and drew
nothing would leave a title waiting for text from something invisible, which is
worse than a refusal it can read. So `MC_uicGetClass` and `MC_uicCreate` answer
`M_E_ERROR`, the context call answers a token, and the other thirty-eight slots
of the block are accepted no-ops — the same shape the other WIPI platform's
component table has, and for the same reason.

The title that asked takes the refusal and draws its own naming screen instead:
a bordered field holding the default name, with `OK:완료` and `CLR:삭제` under
it, on the platform's own text-entry path.

### Network authentication is a path the titles already handle

Several titles authenticate over the network at startup. The network block
refuses every call, and that refusal is **not** a wall: the authenticating
titles run a state machine that reads its result as a one-character server code
(`'1'`, `'2'`) or a negative transport error, and every negative path ends at
"인증에 실패하였습니다. 다음 실행시 다시 인증을 시도합니다" — a notice the
player dismisses. Behind it the game boots, saves and plays.

That is worth stating because this project also holds patched copies of three
of those titles, each differing from its original in exactly two bytes inside
`binary.mod` (`cmp r0, #0` → `movs r0, #2` for one publisher, `ldr r3, [r2]` →
a branch past the block for another). **The patched copies are not needed.**
All three originals reach the same in-game state on this platform, because the
refusal was already handled. Nothing here has to recognise or defeat an
authentication routine.

### Slots that are accepted without being understood

A slot that a real module reaches, that has no known contract, and that is
answered anyway goes in one place —
`internal/platform/lgt/wipic_unknown.go` — with the reason it is
there, because these fail differently from an
unimplemented slot: an unimplemented one stops the game and says so, while one
of these lets the game believe it was served. A slot earns its place only by
being reached by a real module, and a test fails when one is added without a
reason or when a recorded slot turns out to be implemented after all.

The list is meant to shrink, and it has: **it is now empty.** Three entries
left once the specification named them, a fourth left when its argument shape
did, and the last left when its caller was disassembled. Both of the
media-block slots that got that far sit past `MC_mdaVibrator`, where the
ordering is a vendor's own and matches no edition of the specification's list,
so neither could be read off its number.

**`0x4c2` is `MC_mdaSetWaterMark`.** One title calls it with `(clip, 100)`
between creating a clip and playing it, and does not read the result. Exactly
one media function takes a clip and a 0-100 value that is not a volume: the
watermark, the point at which a streaming clip raises `END_OF_DATA` or
`FULL_OF_DATA`. It returns void, which is why nothing branches on it, and its
default is 100% — so the call is a title asking for the value it already has.
The specification carries it in the HAL appendix rather than in the ordered
`MC_mda*` list, which is why the ordering never reached it. Nothing here
streams, so there is no event whose threshold it would move; the slot is
implemented as a clip-handle check and nothing else (`wipic_media.go`).

**`0x4ce` is the default volume of a system volume category, asked for by
name** — the specification's `MC_mdaGetDefaultVolume`, whose own argument is a
numbered category id where this vendor took the name.

What it took to see that was reading the caller rather than the call. The trace
says `0x4ce(name, 2, 0xffffff)` and shows nothing done with the result, and for
several sessions that was the whole evidence: an argument list of three whose
last two constants settled nothing, and a return value nobody wanted. The code
at every one of those call sites — the same vendor SDK routine compiled into
each module, byte for byte across three title families — says something else:

```
movs r0,#0 / bl backLight   ; leaves 2 in r1, and r2 holds what it held
ldr  r0,[pc,#..]            ; the only argument: "GENERAL"
bl   0x4ce
adds r1,r0,#0               ; the answer, straight into
bl   <clamp to 0..100, store as this object's volume>
```

One argument, and the result is read. **The `2` and the `0xffffff` are the
registers `backLight` left behind**, and the clamp to 0..100 is the WIPI volume
scale naming what comes back. `"GENERAL"` is the specification's first system
volume category — the set a handset reports through the `"DEFAULTVOLUME"`
property, `GENERAL, VOICE, RING, KEY, MESSAGE, ALARM, ALERT, MMEDIA, GAME,
OEM` — and it is the one an ordinary application plays under. Every local
caller asks for that one.

The slot answers the system volume, which is what `MC_mdaGetVolume` reports and
what `MC_mdaSetVolume` moves, so a title that reads the level and restores it
on the way out round-trips. An unknown slot answered zero, which told six
titles they had been started silent. **Nothing observable changes in the local
archives**: their first nine hundred ticks make an identical sequence of
platform calls either way, because this platform does not attenuate by a volume
and no local title has yet branched on the level it stored. The correction is
worth having anyway — a stored zero is a zero a title can hand back through
`MC_mdaSetVolume`, and then it is this platform's volume too.

**A trace reports registers, not arguments.** That is the lesson worth carrying
out of this one: where a slot's argument shape is what has to settle its
contract, the trace is the pointer to the call site and the call site is the
evidence.

### Character input

The five slots at `0x12c`–`0x130` are the specification's input-method calls,
in its own order:

| Slot | Call |
|---|---|
| `0x12c` | `MC_imGetSurpportModeCount` |
| `0x12d` | `MC_imGetSupportedModes` |
| `0x12e` | `MC_imSetCurrentMode` |
| `0x12f` | `MC_imGetCurrentMode` |
| `0x130` | `MC_imHandleInput` |

They were read as a record database for several sessions, on the arithmetic
that the specification's third section is the database. What settles it is the
callers, and one engine's text-entry widget makes four of the five calls in its
constructor:

```c
count = 0x12c();                                  /* stored in the widget   */
modes[0..3] = 0, 1, 2, 3;
p = 0x12d();
if (strstr(p[0], "/L"))      { modes[0] = 0; modes[1] = 1; }
else if (strstr(p[0], "/S")) { modes[0] = 1; modes[1] = 0; }
widget->mode = 2; 0x12e(2);
```

`0x12c` is called with no arguments set up at all — the registers still hold
the widget's own — which is what a call to a `void` function looks like, and
its result is kept as a count. `0x12d` answers a `char **` the widget
dereferences and hands to `strstr`. And `"/S"` and `"/L"` are not the widget's
invention: the specification says in as many words that a supported mode is an
ISO 639 language code, that a script with case appends `"/S"` or `"/L"`, and
that digits are `"N123"`. The fifth call is made from the widget's key handler
with six arguments — four in registers and two on the stack, `(key, type,
buf1, size1, buf2, size2)` — which is `MC_imHandleInput`'s signature exactly,
and was the "caller that looks like a record read" that kept the database
reading alive.

So the four cheap ones are answered as the specification defines them:
`EN/L`, `EN/S`, `KO`, `N123`, a mode index that is kept and reported back, and
a `1`/`0` rather than an error code from the setter. A widget ends up in
exactly the state it reached before — the reading it takes from `"EN/L"`
produces the same mode table its constructor started with — which is why
adopting the real contract changed no title's frames.

**There is no automaton, and the null one is a defined answer.** The
specification says what happens to a key the automaton cannot use: whatever is
composing goes into the completion buffer and the call returns 0. An automaton
that composes nothing is never composing anything, so both output buffers come
back empty and every key answers 0. That is what `MC_imHandleInput` does here —
not a stub standing in for the contract, but the contract's own
did-not-handle-it branch, taken always.

**Every local title that reaches this call reaches it once, to reset.** Three
archives make the call, each exactly one time, from a text widget's
constructor, and the key is always `0x9d`. That is `MH_IMA_FLUSH` — the
specification defines it as `-99` and the parameter is an `M_Char` — so the one
thing any title asks for is "finish whatever is composing", asked before
anything has been typed. None of them routes a character key through the
platform afterwards. So what is missing is composition itself, and no local
title is waiting on it.

It stays missing on purpose. The mode that widget selects is Hangul, and
composing Hangul from a twelve-key pad is a handset vendor's own layout rather
than anything the specification fixes.

**The composing buffer is written now, and the reason it was not is worth
keeping.** `(key, type, buf1, size1)` arrive in registers and `(buf2, size2)`
on the stack, and a stacked argument recovered wrongly writes into the game's
own stack rather than failing. There was no caller available to confirm where
it sits; there is one now, and its own code settles it — the widget builds two
eight-byte buffers on its stack, stores their addresses at `[sp]` and `[sp+4]`,
and the platform stub balances its push before the supervisor call, so the
stack pointer the handler sees is the caller's. Both sizes are the caller's
capacities and the specification marks them in-only, so neither is rewritten.

One argument still does not match the specification. The `type` is genuinely
computed — the caller writes `0xfb` and shifts it left, so `0x1f6` is meant —
and `MH_Event` numbers its events from 1 to 11. It is a vendor's own numbering,
and nothing here needs it: a flush is a flush whatever the event was.

Where a later attempt would start: `internal/textinput` already implements the
multi-tap keypad the other platforms' text fields use, which covers three of
the four modes above. It has no Hangul — and Hangul is the mode that widget
selects — and it keeps a whole field's text, while this call hands back a
completed string and a composing one and leaves the field to the widget. So
what it needs is an adapter, not a caller.

Because a Clet writes the framebuffer directly, the runtime's copy of a
surface is **re-read from guest memory before every draw call and written back
after**, and `MC_grpFlushLcd` re-reads before presenting. Without that a game
that mixes direct writes with `MC_grp*` calls would see one of them lose.

## The two arc slots come from their neighbours

`0xd8` and `0xd9` were blank, and a blank slot in this block is not a stub —
`handleWIPICSVC` ends the session on one, so a title reaching for a curve
stopped there rather than drawing a poor one.

Placing them needed no disassembly, which is the anchor rule stated in "The
WIPI C slots" doing its job. `0xd7` is `MC_grpCopyArea` and `0xda` is
`MC_grpDrawString`, both confirmed, and the specification's own function order
puts exactly two entries between them: `MC_grpDrawArc` then `MC_grpFillArc`.
Two names for a gap of two slots, in a known order, between two anchors. The
same argument fixes the other ARM platform's 15 and 16, whose table has the
same two neighbours at 14 and 17.

The signature is the rectangle calls' with two more integers in the middle:

```c
void MC_grpDrawArc(MC_GrpFrameBuffer dst, M_Int32 x, M_Int32 y,
                   M_Int32 w, M_Int32 h, M_Int32 s, M_Int32 e,
                   MC_GrpContext *pgc)
```

Eight arguments, so **the context is the eighth and not the sixth** — reading
it where the rectangle calls keep theirs would take an angle for a context
pointer. `s` is a start angle and `e` is an *extent* rather than an end, zero
degrees is three o'clock, positive angles run counter-clockwise, and the arc is
centred in the rectangle `x, y, w, h` names. A width or height that is not
positive draws nothing, which the specification says outright.

The Java side had the matching hole and a worse one beside it:
`Graphics.drawArc` and `fillArc` were not implemented, and `fillRoundRect` and
`drawRoundRect` were both wired to the plain rectangle — so every panel a title
rounded came out square. All four now walk `internal/curve` and fill through
the same clipped, translated fill the rectangle calls use. See `ktf.md`, "The
same geometry, on five surfaces", for why that geometry is in one place.

## Drawing: the context belongs to the game

Every `MC_grp*` call that writes pixels has the same shape —

```c
void MC_grpFillRect(MC_GrpFrameBuffer dst, M_Int32 x, M_Int32 y,
                    M_Int32 w, M_Int32 h, MC_GrpContext *pgc)
```

— the destination surface first, **the context last**, and the colour is a
field of the context rather than an argument. `MC_GrpContext` is a structure the
game allocates; the platform never holds one. `MC_grpInitContext(pgc)` fills it
with defaults, and `MC_grpSetContext(pgc, index, pv)` changes one named field at
a time.

This runtime used to model it the other way round: the first argument was read
as a handle to a context object of its own, and the last as a colour. Nothing
matched, so every fill, line and string a game asked for was refused, and the
titles that draw their own sprites into the framebuffer looked like they were
working. What that actually cost was every background: a notice screen a game
fills white and writes red text onto came out as red text on black.

The field identifiers are confirmed by the callers: one title sets index 1 six
hundred times while drawing (a 16-bit pixel), index 0 with a pointer when it
changes what it may touch, and index 7 once at startup. Those are the
foreground, the clip and the font. The clip is four `uint16` — left, top, right,
bottom — behind the pointer; a clip that does not describe a rectangle is taken
as the whole surface, because a game that never set one would otherwise draw
nothing.

### A Java title's drawing does not synchronise, and a Clet's has to

A surface here exists twice: as the runtime's `[]uint16` and as bytes in guest
memory at the address the surface was allocated at. Keeping the two in step is
what `syncFromGuest` and `syncToGuest` do, and **which of them is needed
depends entirely on who is writing pixels**:

- **A Clet is handed the framebuffer's address** — that is what
  `MC_grpGetScreenFrameBuffer` is for — and it writes pixels into it directly,
  bypassing every `MC_grp` call. Its draws therefore have to read the surface
  back before they blend and write it out after. Nothing here can be dropped:
  instrumented over 1,200 ticks, two Clets found the guest had changed pixels
  in 600 and 601 of the 1,201 syncs, two million pixels between them.
- **A Java title never gets an address.** It draws only through `Graphics`
  objects the platform hands it, and the runtime's copy is the only one that
  anything writes.

Doing it on the Java path anyway was, for a long time, **the single most
expensive thing this platform did**. Every drawing call read the whole surface
out of guest memory, converted it a byte at a time into `uint16`, drew, and
converted and wrote all of it back — 150 KiB each way for a call that might set
one pixel, and a title makes tens of thousands of them a scene. A host profile
put `syncToGuest` and `syncFromGuest` at **55% of the emulator's time**, against
4.2% for the whole ARM interpreter.

The same instrumentation that priced the Clet case settled the Java one:
**1,711,725 round trips in one title's session and 82,017 in another's, and not
one of them found a single pixel the guest had changed.** They were removed, and
the invariant they were maintaining is restored once a frame instead — the
flush publishes the finished frame to guest memory rather than reading it back,
because on this path the runtime's copy is the newer one. What that assumes is
that nothing on the Clet side touches a Java title's surfaces between frames,
which holds because a Java title registers no Clet and calls no `MC_grp` slot.

Measured over **all 28 local archives**, 900 ticks each, host nanoseconds per
guest instruction before and after:

| | before | after | |
|---|---|---|---|
| the four Java titles | 705.5 / 614.7 / 207.8 / 207.4 | **67.5 / 170.3 / 38.9 / 39.2** | 3.6x to 10.5x |
| the twenty-four Clets | 6.47 – 222.2 | the same | none slower by as much as 2% |

and on the two runs that matter, driven to where a title is actually working:

| | before | after |
|---|---|---|
| one Java title, 1,500 ticks | 701.5 ns a step | **67.3** |
| the other, a whole in-game session | 366.6 ns a step | **41.0** |

That session went from 150.9s of host time to 16.9s — 25.5ms a tick to 2.9ms,
and from 1.96 times the guest clock to 17.5 — while retiring the same
411,577,534 guest instructions. Every frame of both Java runs came back
byte-identical, 750 and 2,484 of them, which is what says this was a cost being
paid for nothing rather than a behaviour something relied on.

### The field numbers are the specification's, and index 3 is a gap

The identifiers are not a guess and do not need to be inferred from callers.
The standard defines them one constant at a time, with the number in the prose:
`CLIP` 0, `FG_PIXEL` 1, `BG_PIXEL` 2, `ALPHA` **4**, `PIXELOP` 5,
`PIXEL_PARAM1` 6, `FONT` 7, `STYLE` 8, `XOR_MODE` 9, `OFFSET` 10. Index **3 is
not assigned to anything** in that list, which is worth knowing because a
reader counting the names off in order lands one short from `ALPHA` onwards and
concludes the platform's numbering is skewed. It is not; the specification
simply skips one.

Two of those constants are also missing from `MC_grpSetContext`'s own table of
indices — `PIXEL_PARAM1` and `XOR_MODE` appear nowhere in it, though the
`ALPHA` row refers to `PIXEL_PARAM1` by name. Reading that table as the
authority on how many fields exist is what produces the off-by-one.

### A pixel operation is a function the game installs, not a colour

Index 5 holds a **function pointer**, and index 6 the parameter it is called
with. A title that sets one is telling the platform how each pixel it draws has
to be combined with the pixel already there:

```c
M_Int32 op(M_Int32 srcpxl, M_Int32 orgpxl, M_Int32 param1)
```

One local title sets index 5 eighty-five thousand times in a run, with values
like `0x2a75` and `0x23bd`. Those look like 16-bit colours, and reading them as
colours is what sent an earlier investigation after a field-numbering error
that does not exist. **They are Thumb addresses** — the low bit is the Thumb
bit, and the module's own code lives in exactly that range. Disassembling the
two settles it and costs nothing, because it needs no run:

- `0x23bc` is eleven bytes long: `if (arg1 == key) return arg0; return arg1;`,
  where `key` is a colour it loads from a global. Plain colour-key
  transparency.
- `0x2a74` does the same key test and then adds the two pixels channel-wise in
  RGB565 — `>>11 & 0x1f` and `>>5 & 0x3f` and `& 0x1f`, clamped at 31, 63 and
  31, shifted back and recombined. An additive blend, which is what an effect
  is.
- `0x2618`, which a run later showed is the one it uses most, is the same key
  test followed by `(incoming - existing) * w + (existing << 8) >> 8` per
  channel, with `w` from a second global. A linear interpolation: an alpha
  blend the title implements itself.

**A title can do its own alpha, which is why the unread `ALPHA` index was not
the gap.** This one never sets index 4; it installs the blend as a function and
leaves the platform's alpha field alone. `MC_GRP_CONTEXT_ALPHA_IDX` is still
stored and not read, and per the specification setting it implicitly sets
`PIXELOP` and `PIXEL_PARAM1` — so a title that uses it instead of installing a
function remains a separate case, and no local title has been seen to.

**Every one of these takes its key and its weight from a global rather than
from `param1`.** A run logs `param=0` throughout, which is not a platform bug
but the disassembly agreeing with itself from the other direction.

**The arguments are not in the order the names suggest, and this platform is
not KTF.** The specification's prose is explicit where its names are not:
`srcpxl` is "프레임 버퍼에 있는 픽셀 값", the pixel *already in the
framebuffer*, and the second is the one about to be written. Its own alpha
example proves it — `(srcpxl * (255 - param1) + orgpxl * param1) / 255`, with
an alpha where 255 means fully opaque, has to answer the incoming pixel at 255.
Both local operations agree: each compares **argument one** against its
transparent-colour key, which is only meaningful if argument one is the pixel
being drawn, since keying on what is already on screen would hide sprites at
random.

KTF's titles pass them the other way round — see "the specification's prose for
those parameters is the wrong way round" in `internal/platform/ktf/wipic_pixelop.go`,
where two operations return their *first* argument in the ordinary case. Both
readings are right for their own platform, and neither should be ported to the
other. `TestDrawRunsTheGuestOperationWithTheFramebufferPixelFirst` pins this
one with two operations that differ by a single instruction: `bx lr` answers
argument zero and leaves the surface untouched, `adds r0, r1, #0; bx lr`
answers argument one and paints it.

Every LGT draw path funnels through one `put`, so honouring the operation is a
change in one place. It runs on the thread already servicing the call, because
the guest is inside a draw and its stack pointer is where it left it, and the
answers are cached by the pair of pixels: the operation is a pure function of
its arguments, and a draw would otherwise call guest code once per pixel. The
drawing modes are exclusive in the specification — setting one cancels the
other — so XOR still wins over an operation rather than compounding with it.

**The cache has to be per operation, not one at a time.** A title switches
operations far more often than is obvious: this one alternates between its
colour key and its alpha blend about twice a tick, so a single cache keyed by
"the current operation" was discarded 3,879 times over 2,000 ticks and gave
back most of what it was there to save. That cost is invisible in a frame — the
output is identical either way — and it was found by counting how often the
platform logged that an operation had been installed. The caches are held
together, bounded at eight operations and 32,768 pixel pairs each; exceeding
either bound drops answers and costs speed rather than correctness.

A word this platform did not write is not trusted to be a function: only a
Thumb address inside the loaded module is taken, because a title that leaves a
handle or a colour in that field would otherwise have the platform branch to an
address that is not code. KTF has a title that stores a font handle in the
equivalent word, which is where that rule comes from.

### What the change actually did to a screen, measured

The change was measured by building the same tree twice, once with the
operation path forced off, and diffing the two runs over one route. 1,938 of
1,998 frames differ, all inside the bounding box of the consent panel that is on
screen throughout, and the whole difference is **one colour pair over 17,361
pixels**:

```
RGB565 (4, 11, 5)  ->  (4, 10, 4)      red unchanged, green -1, blue -1
```

Three things are worth taking from that, and one thing is worth not taking.

- **The operation touches exactly one fill.** The panel's white text, its green
  button and its black pixels are bit-identical between the builds. Whatever
  else this screen draws does not go through the alpha operation.
- **The delta is at most one step of a 565 channel** — the smallest difference
  the display can express, which is why almost every frame differs while the two
  builds are indistinguishable by eye.
- **The result is a fixed point of the blend, and that is the interesting
  part.** `((in - ex) * w + (ex << 8)) >> 8` returns `ex` unchanged whenever
  `in - ex` is 0 or 1, so once a channel has settled one step below the colour
  being drawn it stays there. A panel redrawn every frame through a linear
  interpolation *could* have marched steadily toward the background and gone
  black over a few hundred frames; it does not, and the measurement showing the
  same delta at tick 100 and at tick 1999 is what rules that out.

**What the measurement does not give is the weight.** An earlier reading here
claimed the difference proved a weight at or near 255, on the grounds that a
partial weight would have produced an obvious difference. That reasoning
silently assumed the pixel behind the panel was black. Solving the observed pair
for both unknowns instead, *every* weight from 0 to 256 admits some background
that produces it, so this data pins nothing about `w`. Reading the global it
comes from would need a run, and nothing depends on the answer.

Predicting the per-channel numbers ahead of the measurement went the same way:
green and blue came out at one step as expected, red did not move at all, and
the reason is that red's existing and incoming values were already equal. Which
channels move depends on the pixel already there, so the guarantee is
"no more than one step per channel", not "one step per channel".

So the platform is running the game's own function faithfully, and the visible
result on this screen is a quantisation step. The screens where these operations
do something dramatic are the ones using the **additive** blend at `0x2a74`, and
no route has reached one.

**A title that installs no operation cannot be affected, and that is worth
reading off the code rather than measuring.** The branch is taken only when the
context carries a validated Thumb pointer; every other draw follows the path it
followed before, instruction for instruction. Three titles were run to confirm
it and none of them ever entered the new code — which is the expected result
and, in hindsight, a weaker statement than the one the source already makes.
The population that can be affected is titles that *do* install an operation,
and a run that never reaches one says nothing about them.

That first check only reached opening notice screens, which is the wrong place
to look: a notice screen is exactly where an operation has not been installed
yet. A later pass drove one title through **15,490 ticks of route to real
gameplay** on both builds — no operation installed on either, and **0 of 7,267
frames differing**, byte-identical against a noise floor that is itself zero.
So the inert case is now confirmed at play depth and not only at a title screen.

What that still does not produce is the other half: a title installing an
operation *during* gameplay. The population that could is now measured rather
than assumed: **all 28 local LGT archives were booted for 600 ticks and the
install line was counted, and exactly one title logs one** — the title this
section is about, on its opening screen, with every other archive at zero. A
boot window is the honest limit of that number, since an operation installed
three screens into a game would not be in it; what it does settle is that no
second title reaches for one on the way in, so the population known to install
an operation at all is still one.

**What this still does not have is the screen it was reported for.** The
operations are live, and the alpha one is measured above, but nobody has driven
this title to the battle effects the original report was about — which are the
screens the additive blend serves. What is written here is the mechanism, the
disassembly, and one measured screen; it is not a claim that the effects now
look the way the handset drew them.

### The text face is the handset's small one, on both paths

Text here — a Clet's `MC_grpDrawString` and a Java title's `Graphics.drawString`
alike — is drawn and measured with `glyph.Handset()`, and the three font slots
answer that face's own metrics rather than fixed numbers. It used to be the
16-dot face, which is what a screen every LGT archive declares as 240x320
suggests.

**The screen does not predict the font, and the boxes are what say so.** A game
lays its own layout out of the metrics it was written against and then draws
into it. At 16 dots a notice screen sized by its title for eleven syllables a
line holds ten, and the eleventh is clipped against the right edge of the
screen: the last character of four consecutive lines of one title's usage notice
was cut in half. The same strings fit inside the same box with room to spare at
the handset metrics. That is the same finding the KTF runtime's `fontFace`
records, reached independently on this platform's own archives, and it is one
face for every screen size for the same reason.

Three of the twenty-four local archives draw any text through this platform at
all — the rest carry their own bitmap font and their own layout, and are
byte-identical across the change. That is worth knowing before reaching for the
font to explain a screen: a frame diff across the whole library named the three
in one pass, and a title that was not one of them was never going to be fixed
here.

### Magenta is the mask colour, and a surface copy leaves it behind

`0xf81f` — pure magenta in RGB565 — is transparent to `MC_grpCopyFrameBuffer`
and to `MC_grpDrawImage`. It is a property of the platform, not of a call:
nothing in either argument list carries a colour key, and the titles here never
set the context's transparency field.

What settles it is what a caller has to be able to do. One engine renders all
of its text this way:

```c
MC_grpFillRect(strip, 0, 0, w, 10, pgc);     /* foreground = 0xf81f */
p = MC_grpGetFrameBufferPointer(strip);      /* draws the glyph itself */
MC_grpCopyFrameBuffer(screen, x, y, 12, 12, strip, sx, sy, pgc);
```

It has no other blit, and its outlined-text path is the same copy run four more
times at one-pixel offsets. Copying opaquely puts a magenta box behind every
character on every screen of that title, which is exactly what it did before
this was understood — the game looked like a palette fault.

The mask is the answer of last resort, not the first one. A surface that
carries transparency of its own keeps it: a decoded image whose encoding
declared which pixels are transparent — a PNG's alpha, or the palette entry a
BMP names in its header (see `wipic.DecodeBitmap`) — is drawn by that
declaration alone, because a picture allowed to say what it means is allowed to
paint in the mask colour. A handset bitmap decoded through `wipic.DecodeLBMP`
declares nothing — the format has no transparency this build reads — so the
mask is what one gets; see `ktf.md`, "LBMP: the handset's own bitmap", for what
that format is and why every platform's image router now reaches it. The mask
applies only where nothing was declared,
which is the case that matters: the surfaces a title builds for itself have no
encoding to declare anything.

The risk of a fixed key is a picture that wanted a genuinely magenta pixel and
gets a hole instead. That risk is the reason the convention exists: art for
these handsets avoids the key colour. It was measured rather than assumed —
turning the mask on left the frames of six other local titles **byte for byte
identical**, so nothing here paints with it by accident. The title it was found
on ships neither PNG nor BMP: its art is in formats it decodes itself, writing
pixels straight through the framebuffer pointer, so there is no header that
could have declared anything.

## Timers

`MC_knlDefTimer(MCTimer *tm, TIMERCB cb)` registers a callback **against the
game's own timer structure**, and `MC_knlSetTimer(MCTimer *tm, M_Int64 timeout,
void *parm)` arms it. The callback is `void (*)(MCTimer *tm, void *parm)`.

Two things about the arming call are worth writing down, because getting either
wrong runs the game's data as code:

- The 64-bit timeout occupies `r1` and `r2` **without** the even-register
  alignment a modern ABI would give it, so the parameter lands in `r3`. Reading
  `r3` as the callback — which is what this runtime did — branches to whatever
  the game passed as its parameter. It faulted inside the game's own data with
  an address that named nothing.
- The callback belongs to the structure, not to a handle: a title defines a
  timer once and arms it a thousand times, passing only `tm`.

## Input

A key reaches a Clet through its `handleCletEvent`, and **the event kind is the
WIPI Java key event type plus 501**: 502 pressed, 503 released, 504 repeated.
Both engines here agree — one subtracts `0x1f6` and `0x1f7` from the kind and
branches on zero, the other compares against `0xfb << 1`. The key code itself is
the WIPI value a card's `keyNotify` would have received, which is the same set
KTF uses, so a Host that maps keys for one platform maps them for both.

Sending 1 and 2 delivers events every title ignores, which from the outside is
indistinguishable from input not being wired up at all.

### Nothing repeats a held key, and no title has asked it to

The specification has a third kind — `MV_KEY_REPEAT_EVENT`, "fired every so
often while the key stays down" — and a `KEYREPEAT` system property that
answers `"600:250"`-style timings, meaning the first repeat after 600ms and one
every 250ms until release. This platform generates neither: a key is a press
and a release, and `KEYREPEAT` is not among the properties.

That was worth checking rather than assuming, because a browser keypad is held
down with a thumb and a game that only acts on presses would move one step per
tap — which is what "movement is jerky" would look like. Two measurements say
it is not the case here:

- **No local archive contains the string `KEYREPEAT`**, across all 64 KTF, LGT
  and SKT archives and the JARs inside them. Nothing asks for the cadence.
- **A held key already moves a character across a map.** Pressing a direction
  without releasing it, on the title whose movement was reported as stuttering,
  walks it out of the starting area and into the next one in 120 ticks. The
  titles track the press and the release themselves.

So repeats stay unimplemented for the reason anything else here does: there is
no caller, and a cadence invented for none would be a contract this platform
would then have to keep.

### A facing is read off the sprite, and it needs magnifying to be read at all

One title was reported turning the wrong way: pressing left and attacking left
turns the character to the right. Nothing in a trace answers that — a facing is
the title's own state and it reaches the platform as sprite blits — so the
answer has to come off the screen, and **a character on a 240-wide handset
screen is about twenty pixels tall.** At 1:1 a contact sheet cannot tell one
facing from the other; at five times, with pixels repeated rather than
smoothed, the eye and the sword are unmistakable. That is what
`wfeature zoom` is for ([`cli.md`](cli.md)), and it is the thing that had been
missing rather than any run.

With it, the title's facing is **correct in isolation**: a short press of left,
of right and of up each turns the character that way, the attack that follows
keeps the facing it was given, and the frames after the attack keep it too.
The report is about an attack with an enemy on the screen, so what is left is
to reach one — the facing itself is no longer the unknown.

## The guest clock advances with work, not only with ticks

`Session.Tick` moves the clock by one tick. That is not enough on its own: a
title's loading screen spins on `MC_knlCurrentTime` waiting for a deadline, and
it does that **inside one Clet call**, where the Host cannot get a tick in. The
clock never moved, the wait never ended, and the run died on the instruction
ceiling with the same timestamp on every read.

So the clock also advances with instructions retired, at a rate of the order of
an ARM9 handset (`guestInstructionsPerMillisecond`). The work is measured from
the last tick rather than from the start, so the two do not compound over a run:
a tick sets the floor and moves the baseline, and instructions carry the clock
from there. What the rate really sets is what a spin-wait costs — a game waiting
100ms spends about five million instructions on it.

**The floor is the larger of the two, not their sum**, and getting that wrong
is not a rounding error. A tick is how much time the Host says has passed; the
work is how much of it the guest was busy for. A frame that spends 43ms of
guest work inside a 50ms tick took 50ms, 7ms of it idle. Adding them made it
93, which ran every title here at about **1.9x speed** — every animation,
timer, cutscene and spawn rate uniformly wrong, and nothing about a screenshot
that shows it. Only when the work overruns the tick does the work win, which is
the case the work clock exists for: a loading screen that burns three seconds
of instructions inside one call moves the clock three seconds.

The measured numbers, for anyone checking this again: a title in the world
retires about 2.16M instructions per tick, which is 43ms of work against a 50ms
tick; a world load retires 165M in one, which is 3300ms.

### The Host has to pace the ticks, because nothing else does

The clock above is virtual end to end: it moves when a tick moves it and when
the guest retires instructions, and never because time passed. That makes the
Host's tick rate the game's speed, and a Host that waits a fixed span between
ticks sets that speed to `tick / (its own tick cost + that span)` — a ratio
between two numbers neither of which is about the game.

It fails in both directions and looks like two unrelated bugs. Through cheap
menus, where a tick cost 10ms against a 16ms fixed wait, a title ran at
**1.79-2.48x**. In-game on a slower machine, where a tick cost 57.7ms, the same
title ran at **0.68x** — slow motion at a rock-steady frame rate, because
nothing was dropping frames; the guest was just told less time had passed than
had. A steady frame rate is what makes this hard to see: it does not look like
a Host falling behind, it looks like the game being that speed.

`Session.TickFor` is the paced entry point and the one a Host on a real clock
must use: a tick is due every session tick of wall time, and it reports the
wait that is left. Zero once the tick overruns, since a tick that costs more
than it represents cannot be bought back, and the debt is capped at one tick so
that a world load inside one call is not repaid by sprinting through the scene
after it. Measured at 1.00x steady, 20 ticks a second. `Session.Tick` still
steps unpaced, which is what a batching Host — `runlgt`, the acceptance probes
— wants: the same sequence, only faster.

### A tick that stands for a fixed span rounds every frame rate up

Pacing the ticks fixed the ratio; it left the granularity, and the granularity
was most of the reported "everything on this platform is slow".

**A title's frame loop here is a timer that re-arms itself at the end of every
frame**, so the interval it asks for *is* the frame rate it wants. What the
local titles ask for, read off `MC_knlSetTimer`:

| title | asks for | got, on a 50ms tick | now |
|---|---|---|---|
| two of one series | 46ms | 101.7ms | 56.9ms |
| a fighting title | 1ms | 50.0ms | 19.0ms |
| a side-scroller | 83ms | 100.0ms | 83.1ms |
| a dungeon title | 60ms | 102.2ms | 80.1ms |
| a platformer | — | 101.0ms | 72.1ms |
| a licensed title | 20ms | 50.1ms | 27.3ms |

A tick that always stands for 50ms can only ever fire a timer on a 50ms grid,
so **every interval is rounded up to a multiple of the tick** — and the
rounding is worse than it looks, because the guest arms the timer partway
through a tick. One title asks for 46ms, arms it about 9ms into a tick, lands
5ms past the next one and waits a whole further tick: 100ms of guest time for a
frame it wanted at 55. Half speed, with a rock-steady frame rate and nothing in
any number saying so — the same shape as the ratio bug it hides behind.

The fix is that **a tick stands for the wait until the guest's next scheduled
work**, bounded above by the session tick, and `TickFor` waits for the span the
tick actually stood for. The ceiling is what keeps a Host taking frames and
delivering keys while a game waits out a long timer; nothing else about the
clock changes, and a title with no timers armed ticks exactly as before.

**A floor belongs on the period, not on the tick.** A title here asks for a
one millisecond timer, which is not a request for a thousand frames a second —
it means "as soon as you can", and on a handset the answer was its own frame
time rounded up by whatever the timer could resolve. The specification says
exactly that: a timer is accurate to "the timer resolution the operating system
underneath supports", and error is expected. The guest's own work is the first
bound and the honest one — the work clock measures it and `advance` takes the
larger of the two — but that answers nearly zero for a title idling behind such
a heartbeat, so the period is also floored at one frame of a sixty hertz
display. That number is deliberately not a claim about any handset; it is the
fastest a screen anyone plays this on can present. **Flooring the tick instead
was tried and is wrong**: it rounds every interval up to a multiple of the
floor, which is the original mistake in miniature — the 46ms title came out at
64.5ms rather than 56.9.

Measured across the local set with `-ticks 600`, before against after, as guest
milliseconds per frame: 101.7→56.9, 50.0→19.0, 50.1→47.6, 100.0→83.1,
102.2→80.1, 101.0→72.1, 102.3→68.3, 230.0→200.0, 50.1→27.3. Nothing is slower.

### A Java title's frame loop is not a timer, and it lost a tick a frame twice

Everything above is about `MC_knlSetTimer`, which is a **Clet's** frame loop. A
Java title arms no timer: its loop is a thread, and it ends each frame with
`Thread.yield()` and `Thread.sleep(n)`. Both of those were costing it a whole
session tick per frame, for two different reasons, and the report that found
them was a player's — "it is a little faster, but it still lags when I move" —
against a build whose host cost had just come down ninefold.

**The tick did not know when the thread wanted to wake.** `tickSpan` asked
`nextTimerDue` and nothing else, so a title with no Clet timer always got the
full session tick. Now it asks `nextJavaThreadDue` as well and takes whichever
comes first — including **zero**, for a thread whose deadline has already
arrived. That last part is the whole of it: the tick that services the wake
advances the clock before handing over the slice, so without it every wait came
out one tick long. A thread that merely spent its budget is skipped, since zero
there means "no deadline" rather than "due now", and treating it as due would
stop the guest clock.

**And `Thread.yield()` parked.** Parking ends the slice, and a slice is granted
once a tick, so a yield with no other thread to hand the turn to bought nothing
and cost 50ms of guest time. One local title's loop is `work; repaint; yield;
sleep(100)` — one of each per frame — and it was being charged **150ms of guest
time for the 100 it asked for**, which is a third of its frame rate spent on a
hint. A yield now only parks when another thread could actually run.

That needs a bound, and finding out why is the other half of the lesson: a loop
that yields is not always a frame loop. Another local title spins on `yield`
while it waits, and letting that through unparked spent the whole four-million
step budget every tick — 26.7 million instructions became 4.8 **billion**, and
a run that took seconds took minutes. `javaYieldBurst` caps a slice at eight
free yields, which leaves the frame loop's single one alone and parks a spin
almost at once.

| the reported title, in game | before | after |
|---|---|---|
| guest ms a frame | 122.7 | **86.4** |
| frames a second | 8.15 | **11.6** |
| what its sleep asks for | 100ms, given 150 | 100ms, given 100 |

Host cost moves the other way and that is the right direction: the same session
costs 21.6s rather than the 16.9s the section above measured, because the
emulator is no longer idling through ticks it had no reason to wait out — it is
drawing 35% more frames in the same guest time, at 40.4ns a step. Twelve times
the guest clock, with the frames the title actually asked for.

**Neither of these is visible in host time, which is why they outlived a
profile.** The emulator was not working harder for the slow frames — it was
idle, waiting out a tick it had no reason to wait for. `ns_per_step` does not
move, `busy_ms` does not move, and the only number that says anything is guest
milliseconds per frame.
**In the world, where the report came from, one title's frame goes from 127.3ms
to 87.1ms** over the same scripted route — 1.46x, and what is left there is
throughput rather than pacing, which is the engine-hook question below.

`runlgt`'s summary reports `guest_ms` for this: the tick count no longer says
how much guest time a run covered, and guest time against flushes is the rate a
title is being given.

The step budget for one Clet call is sized for the other end of the same
problem: entering the world happens inside a single timer callback, and the
heaviest local title decompresses over a thousand resources into freshly
allocated images to do it. Fifty million instructions stopped it part way and
read as a hang.

**The budget is measured rather than chosen**, by bisecting with
`runlgt -steps`: that heaviest load falls between 1.1e9 and 1.2e9 instructions,
and the budget is set at about two and a half times it. Sizing it by eye is how
it came to be 1e9 — close enough to look generous, and one title short of the
world. When a run does report the limit, **read the platform-call trace before
raising it**: a callback that is allocating, reading resources and copying is
doing work and wants a bigger budget, while one whose trace is the same handful
of drawing calls over and over is a spin that no budget ends. Raising the
ceiling twenty-fold on one such spin bought twenty times the frames and nothing
else.

## Files

A file is a save entry under the `fs/` scope — the same one KTF's guest
filesystem and SKVM's `XFile` use — falling back to the packaged JAR resource
of the same name. That is how a game reads data it shipped and writes data it
did not:

```
var/savedata/<profile>/lgt/<PID>/fs/<path>
```

There is no record database beside it. `0x130` was serviced as one until the
block it sits in was identified as the input method's; no title here imports a
database call at all.

**A removed file has to be recorded as removed.** The save boundary has no
delete and a read falls back to the packaged JAR, so `MC_fsRemove` writes the
path into a list — `fs/.removed` — that a read consults before either. This is
not bookkeeping for its own sake. Removal used to write an empty file in the
path's place, which left the path existing, and the shape of a title's new-game
flow is:

```c
sprintk(path, "Save%d.dat", slot);
MC_fsRemove(path);
... // create the character
if (MC_fsIsExist(path) == 0) { /* a save is here, so this is a continued game */ }
```

A title told its save is still there **skips everything a new game begins
with**. Two titles here lost their entire opening sequence to it and dropped
the player into the world with no prologue and no tutorial — a failure with
nothing wrong on screen, which is why it survived a run that reached the world
and saved.

RMS keeps the same kind of index for the same reason: a `SaveStore` only
answers about keys it is asked for, so anything the store cannot represent has
to be written down beside it.

The other WIPI platform's guest filesystem had the same shape and was corrected
with it: `FileSystem.unlink` dropped the in-memory entry while the read behind
it still found the persisted save and then the mounted archive. No KTF title
here has been seen to delete and re-ask, so that one is a correction made from
this platform's evidence rather than from a failure of its own — see
[`ktf.md`](ktf.md).

**Read-only is the one open flag that cannot create.** `MC_fsOpen`'s flags are
read (1), write (2, which appends), write-and-truncate (4) and read-write (8);
anything but the first is an open with write intent, and a file that is not
there yet is created by it. Treating a single bit as "writable" refused the flag
a title actually opens its save with, and the title then retried forever against
a handset it read as full.

**An empty save tree is not a neutral starting point.** One title's boot reads
its settings file, and when the open fails it writes the defaults out, clears
the byte it just wrote, and paints a screen telling the player to restart the
handset — then quits on the next key. The next launch reads the file back and
boots into the game. That is a first-run flow, not a fault, but every probe here
starts from a clean save directory, so the title only ever showed the notice and
was carried as broken across several sessions. **A title that stops at a screen
no one can get past is worth launching twice against the same `-save` tree
before it is called a defect** — the first run may be the one that writes what
the second one needs.

`MC_fsFileAttribute` fills three words and **the size is the third**. That comes
from the caller rather than from a header: a title wraps the call twice, once
returning the word at offset 8 and once the word at offset 4, and reserves
twelve bytes for it. The one the callers act on is the size — they allocate
their read buffer with it.

`MC_fsTotalSpace` and `MC_fsAvailable` **take no argument**: they report the
storage the program has, not the remainder of an open file. Reading the first
register as a file handle answered an error to a title asking how much room it
had, and it refused to start a game because the handset was full. The number
they report is this platform's own — comfortably larger than any save here and
small enough to stay an ordinary integer.

`MC_fsMkDir` and `MC_fsRmDir` report success without doing anything, because a
path is a save key and there are no directories to make.

## Sound

The media block is at `0x4b0`, and its order is the specification's `MC_mda*`
list with one entry missing. Four slots pin that alignment independently, which
is what makes the rest readable: `0x4b0`/`0x4b1`/`0x4b3` are clip create, free
and `putData` at indices 0, 1 and 3, and past index 3 every entry sits one slot
below the number the specification gives it — `0x4bd` is stop (14) and `0x4c1`
is the vibrator (18), both established from title behaviour. The volume pair
sits between them, which is what makes `0x4bf` and `0x4c0` readable as
`MC_mdaGetVolume` and `MC_mdaSetVolume` rather than a guess.

A clip is a guest record so that a game can hold the `MC_MdaClip*` it was given
and pass it back, but **the sound's bytes stay on the Host**: the guest never
reads them back, and a megabyte of audio in the guest heap is a megabyte the
game cannot have. Playback goes through `backend.Audio` on the guest's own
clock, advanced once per tick, so a run batching ticks hears the same sequence
as one on the wall clock, only faster.

Two things about this block are easy to get wrong and silent when you do:

- **The last four slots take a source, not a device.** `MC_mdaSetMuteState`
  and `MC_mdaGetMuteState` are keyed by source, and the pair in front of them
  reads as the same thing for volume — one title calls `0x4d0(11)`, refuses to
  go on if it answers -1, hands what it got to `MC_mdaSetVolume`, and then
  calls `0x4cf(11, level)`. Reading a per-source mute as a global one silences
  every clip in the game: a title mutes source six during its own startup and
  then plays.
- **Volume is a percentage.** One title sets a clip to 50 and another to 60.
  Nothing here attenuates by it — the synthesiser's velocities carry the mix —
  so what the level has to do is come back out the way it went in, because a
  game reads the level it found and restores it on the way out.
- **`0x4ce` answers a named category's default volume**, and answering zero for
  it told six titles they had been started silent. It takes the category name
  and nothing else; what looks in a trace like two more arguments is the
  registers the preceding `backLight` left behind. The slot is
  `MC_mdaGetDefaultVolume` with a name where the specification has a numbered
  id, and "Slots that are accepted without being understood" has the call site
  that settles it.

A Java title also registers a `PlayListener` on a clip, and this platform
**takes the registration and delivers nothing to it**. The specification's
events are ERROR, END_OF_DATA, START, STOP, PAUSE, RESUME, RECORD and
FULL_OF_DATA; of those, the only ones a title here could act on are START and
END_OF_DATA, and the mixer behind this block has no end-of-clip signal to raise
the second from. So a delivery would either be invented or would never come.
Recording the listener is what lets the titles that only register one go on —
one of them stopped dead at `Clip.setListener` and now runs — and a title that
*waits* on an event will stop where it waits, which is a better place to find
out than a callback made up out of a clip's byte count. The listener is kept
because the delivery is the next thing to build here and it needs somewhere to
send.

`wfeature runlgt <game.zip> -audio out` records what a run played, which is how
this was checked without a speaker: `out.mid` for the MIDI events and
`out.wav` for the sampled ones.

## Images

An `MC_GrpImage` is a framebuffer with a transparency mask beside it. That is
not a shortcut — `MC_grpGetImageFrameBuffer` exists precisely because an image
is one, and the title that reaches this asks for the framebuffer and then for
its raw pointer, so an image that was not addressable memory would fail there
instead. Images and surfaces share one handle space, which keeps
`MC_grpGetFrameBuffer*` working on an image without a second lookup, and it is
why the LCD carries a flag of its own: nothing else stops a destroy aimed at
an image from taking the screen.

- **`MC_grpCreateImage(MC_GrpImage* out, bufID, off, len)` writes the image
  through its first argument** and returns a status. Getting that backwards is
  silent: the caller reads whatever its stack slot held and carries on with a
  number that was never a handle, which is what one title did for a hundred
  instructions before it asked to allocate a negative number of bytes.
- **Properties four and five are width and height**, which is what the titles
  on both WIPI platforms read them as.
- **`MC_grpDrawImage` is `MC_grpCopyFrameBuffer`'s argument list exactly**, and
  the mask is the whole difference between the two. RGB565 has no alpha
  channel, so an image drawn without its mask arrives as a rectangle of
  whatever its transparent pixels happened to decode to.

### A surface's handle is an address, because one title dereferences it

`MC_GrpImage` is a `void *` in the specification, and one title reads it as
one. Straight after a create it asks for the image's width and height — which a
counter would have answered perfectly well — and then loads the first word of
what the handle points at and stores into that structure at `+0x24`. Against a
handle that was the small integer a counter had reached, that is a read of
guest address `0x4`, and the title died there every run, one screen past its
connection-failure notice.

So every surface is given a record in guest memory and **the address of that
record is its handle**. Word zero is what `MC_grpGetImageFrameBuffer` answers,
so a title that takes the framebuffer out of the structure rather than through
the accessor gets the same thing the accessor would have given it; the three
words after it are the data pointer, the bytes per line and the dimensions the
specification says a framebuffer holds, in that order. The rest of the record's
64 bytes is a vendor's business and stays zero. What matters is not that the
layout past word zero is right — nothing here knows it — but that it is mapped
and the title's own, so a store into a field this platform never chose lands in
the image instead of off the map.

What the title stores is worth keeping: `0x00ff00ff`, which is the magenta this
platform already treats as the mask colour. The write is inert here, and it is
independent evidence for that choice (see "Magenta is the mask colour").

With the handle addressable the title runs on through its publisher screens,
its title art, character select and a prologue, into the world.

### `MC_grpGetDisplayInfo` takes the display index first

The structure is its **second** argument, the same way `MC_grpRepaint` leads
with the LCD. Reading the first argument as the pointer wrote nothing at all —
the index is zero for the primary display — and the title then read its own
uninitialised structure. That is not a call that fails: one title took four
bytes per pixel out of the stack residue and drew every screen at twice its
width, on a framebuffer whose other half it never touched. The screen was
legible enough to look like a palette problem.

`MC_GrpDisplayInfo` is nine words: bpp, depth, width, height, bytes per line,
colour type, and then the red, **blue** and green masks — not the usual order.

### `0xf0` is `MC_grpFillPolygon`, and the block has two unnamed slots in it

A title's tutorial reached `0xf0` and stopped there. The graphics block runs
from `MC_grpGetImageProperty` at `0xc8` in the specification's own function
order, so the number is a name — but only once the block's own gaps are
counted, and it has two. `MC_grpCopyArea` is the fifteenth function and sits at
`0xd7` rather than `0xd6`; `MC_grpPostEvent` is the thirty-seventh and sits at
`0xee` rather than `0xed`. Both are fixed by the identified slots on either
side of them — `MC_grpDrawImage` at `0xd5`, `MC_grpDrawString` at `0xda`,
`MC_grpDecodeNextImage` at `0xeb` — so the offset is +1 after the first gap
and +2 after the second. The last two functions in the block are
`MC_grpDrawPolygon` and `MC_grpFillPolygon`, which lands them on `0xef` and
`0xf0`.

The call settles it without needing the arithmetic to be trusted:

```
wipic 0xf0 unnamed(0x30025a58, 0x400fff80, 0x400fff70, 0x4) = failed
```

The first argument is the same surface handle the title had just asked the
width of, the next two are consecutive stack addresses, the fourth is four, and
a fifth follows on the stack — `MC_grpFillPolygon(dst, xPoints, yPoints,
nPoints, pgc)` with a quadrilateral, and nothing else in the block takes that
shape. The colour comes from the context, which the title had set two calls
earlier. Both forms are implemented: the outline through the same Bresenham
every other line uses, the fill by the even-odd scanline rule the KTF runtime's
polygon takes, so a shape drawn through either platform covers the same
pixels. The point count is bounded — the arrays behind it are the game's own
memory and a count it computed wrongly has to cost a refused call.

### `sprintf` is a C library slot of its own

`0x3f7` is `sprintf`, distinct from the kernel's `MC_knlSprintk` at `0x65` and
served by the same renderer. A title formats the path of the resource it is
about to open with it.

### The rest of string.h, placed by its neighbours

An LGT title ran for two minutes and then stopped on `unimplemented LGT stdlib
slot 0x40b`, from inside a timer callback. The slot number is enough to name it
without disassembling the caller, because the C table is the specification's
own list in the specification's own order, and both ends of the gap were
already known:

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
strncmp, strchr, strrchr, strspn, strcspn, strpbrk, strstr, strlen, strtok`,
and counting that list from `strcpy` at `0x405` lands `strstr` on `0x410` and
`strlen` on `0x411` — the two slots at the far end, both of which titles do
call. **The six in between are therefore not free to be anything else**, so
they are implemented together rather than one crash at a time.

The run stops being forced immediately after that. `strtok` would be `0x412` by
the same count, but `memcpy` is `0x414` rather than the `0x413` the list
predicts, so one unaccounted slot sits somewhere past `strlen` and everything
after it is a guess. `strtok` is left unimplemented for that reason, and
because it is the one function in the list that carries state between calls —
the wrong contract there would corrupt a title's parse silently instead of
stopping it.

`strchr` and `strrchr` search the terminator too, so a call asking for `\0`
answers the end of the string rather than nothing; a caller measuring a
string it already holds a pointer into depends on that.

The trace then confirmed the name from the calls themselves. The title asks
`0x40b(buffer, 0xa)` and feeds the answer **plus one** back in as the next
call's buffer:

```
stdlib 0x40b strchr(0x400ffeb8, 0xa, …) = 0x400ffed2 from 0x191b1
stdlib 0x40b strchr(0x400ffed3, 0xa, …) = 0x400ffeec from 0x191c1
stdlib 0x40b strchr(0x400ffeed, 0xa, …) = 0x400fff07 from 0x191c1
```

That is a message split on `\n` line by line, which is `strchr` and not
`strrchr` — a backward search walked this way would not move. Replaying the
reported session's own key presses as a route reaches the two-line message box
the title draws with it: **that box is the screen the run used to die on**, and
the count for the run is 491 calls served.

## The Java interface table

Some local titles are not Clets — four archives so far, and the count has gone
up each time archives were added. Their application code is Java, compiled
ahead of time to the same native ARM the rest of the module is, and the class
metadata a class file would carry is handed to the platform through table
`0x64` instead. None of them runs here. What follows is what they were
observed to say, because the table is named by nothing — not the
specification, not the original runtime, not the reference implementation —
and reading it out of a running title is the only way it gets known.

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
`0x1f8`/`0x17` is the one entry of the OEM table that only a Java title
resolves. They are all carried on the Java category here, since none of them
has anything to do with the calls a Clet makes.

The four `0x03` calls are one registration repeated on four tables — the
module loads the launcher's name, `""` and `"true"` into `r0`–`r2` once and
calls all four without touching them again, which is also why the argument
counts above are three rather than the four a trace shows: the fourth register
is left holding whatever the previous call put there. `0x1f8`/`0x17` fails
loudly in the module itself — it prints `don't get jar path` through the
stdlib — so its answer is one the module checks. The two `0x16` calls carry
this platform's screen size as `(0x64, 240)` and `(0x65, 320)`.

A module also carries a small vendor section — `.raptor`, four bytes `RAPT`, a
version, the archive's AID and a feature list that reads
`kernel dlet cldc wipijava lgte midp mmpp` — which is what names the AOT
toolchain that produced everything below. Every Java archive here was built by
the same version of it, so the layouts in this section are one toolchain's ABI
rather than one title's.

The eleven arguments of `0x14` are six inputs and five outputs. The inputs are
`(classes, fields, staticFields, virtualMethods, methods, staticMethods)`; the
outputs are five arrays in `.bss` — and **there are five for six because the
class table gets none.** Output *n* belongs to input *n+1*: the arrays report
fields, static fields, virtual methods, methods and static methods, in that
order. That is not read off the argument order alone, which would put the
outputs one earlier; three independent things settle it, and the argument list
is the weakest of them (`internal/platform/lgt/java_metadata.go`).

**Four of the five are `int16`; the last one is a table of function pointers.**
The four are indices — a caller loads one with `ldrsh` — and the static-method
array is loaded with `ldr` and branched to:

```
ldr  r4, =static_method_out
ldr  ip, [r4, #0xd8]        ; entry 54, a word
mov  lr, pc ; bx ip         ; called
```

So for a static or special method **the platform writes the entry point
itself**, and for everything else it writes the number the compiled code will
index with. That is the difference between the platform being asked where
something is and being asked what to run.

**The six input tables are one contiguous run of memory**, in the argument's
own order, and that is what gives each one its length: static methods end where
methods begin, methods where virtual methods begin, and so on. In one local
title they are 111, 3, 92, 1 and 47 entries. The class table's own runs only
account for 111, 3, **34**, 1 and 0 of those, and the difference is the point:

- The virtual table's first 34 entries are the platform classes the class table
  names, and **the remaining 58 are the application's own methods** — the ones
  whose vtable index depends on how large the platform made the class they
  ultimately extend, so the compiler could not assign it.
- Every one of the 47 field entries is the application's own, for the same
  reason: an object's field slots sit after the platform superclass's.
- The static-method table, by contrast, stops exactly at the platform's 111.
  The application's own constructors are called by address; only a platform
  one has to be asked for.

**A name and a descriptor is all a table entry carries, and that is enough.**
Two entries can read `j:I` and three classes can declare `a(I)V`, and neither
is an ambiguity the platform has to resolve, because of what the numbers are
used for. A virtual entry names one *slot*, and a subclass overriding a method
shares its superclass's slot, so every class that answers to that name and
descriptor answers at the same index. A field entry names one *field* of one
class, so giving every entry an index of its own is right by construction; what
it costs is object size, since an object then has a slot for every field
reference in the title rather than for its own class's fields.

The runs in the class table are what say which platform class owns which
entries, and the platform needs that to know what to implement — not to number
anything.

**All five arrays are answered now**, by `linkJavaSurface` in
`java_link.go`: the four index arrays with the identity map, and the
static-method array with one SVC stub per entry. A stub that is reached names
the class, the method and the entry it stands for and stops the title there,
which is what turns "a Java app is not supported" into "it called
`java/lang/Math.abs(I)I`". Every local Java archive gets through both metadata
calls and stops at the launcher.

One caution about the identity map for fields, measured rather than feared: the
four local titles declare 47, 150, 150 and **416** field entries. An object
sized to the largest index is 1.6 KB in the last of them, and a title that
allocates thousands of small objects will feel that. The way out is to size
each class by its own fields — which needs the class each entry belongs to, and
that is the thing the table does not say.

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

Each run is a count in the high half and a start index in the low half, and
the starts are cumulative across the classes that use that table, which is
what confirms the reading. The name/descriptor tables are pairs of `char *`:
`org/kwis/msp/lcdui/Card` claims four virtual methods starting at index 2, and
indices 2 through 5 of the virtual table are exactly `serviceRepaints`,
`repaint`, `getHeight`, `getWidth`; `org/kwis/msp/media/Clip` claims one at
index 0, which is `setVolume`. Thirty classes and twenty-nine virtual methods
line up end to end with no gaps.

The classes in that table are the **platform's** API surface — `org/kwis/*`,
`java/lang/*`, `java/io/*` — not the application's own. So the metadata says
what the app links against and how it will address it, and the app's own
classes arrive elsewhere, most likely through `0x07`.

### The slot that runs a function

stdlib `0x424` is handed one argument — the address of a function inside the
module — and its result is never read. That has the shape of `atexit`, and it
is not one: **it runs the function it is handed, there and then.**

Both readings answer the caller identically, so the two were separated by
running a title each way and diffing what it asked for next. Not calling it,
the module goes straight on to its entry point. Calling it, one extra platform
call appears in between — `0x64`/`0x06`, and its argument is the value `0x07`
answered when the application's classes were handed over. The function is a
three-instruction thunk into the module's own Java startup, and `0x06` is the
only call in the whole sequence that gives the platform back the handle it
issued for the class list. Registering the thunk for exit instead would enter
the application's Java having never acknowledged its classes.

No name for it was found. The specification's list of supported ANSI-C
functions has nothing that runs a callback, and the slot sits past the end of
that list in this table, so it is named in the code for what it was watched
doing. The reference implementation calls its equivalent the same way and does
not name it either.

`stdlib_run_test.go` pins it against a function the fixture plants, since the
difference between the two readings is invisible in the answer and only shows
up in whether the function ran.

**Running it took the caller's stack with it.** A guest call the platform makes
from inside a platform call has to be made on the thread that is *running* —
the one whose stack pointer is where the guest left it — and this one was made
on the platform's own thread, whose saved stack pointer is still at the top.
The function therefore ran over the frame of the module function waiting for
it. What that cost was one word: the launcher builds its `argv` in its own
frame, calls `0x424`, and then hands the platform an `argv` whose first word
had been overwritten by the thunk's locals. The class name it named came back
as a fragment of something else, which reads exactly like a class the module
does not have — and the fix, `callOn` in `client.go`, is one argument. Any
platform call that enters guest code has the same requirement: a class
initialiser, a lifecycle method, a callback the module registered.

**Past `0x424` the title reaches the entry point**, which is the next thing it
asks for: `0x64`/`0x83` with four arguments:

```
0x83("org/kwis/msp/lcdui/Main", 0, 4, argv)   argv = {"Game", "", "true", "true"}
```

The class it names is **the platform's own**, not the application's, and the
four `argv` words are a template the module keeps in `.text`. So the module is
not asking the platform to run its code: it is asking for the platform's Jlet
launcher. What `"Game"` names is not settled — it is the same string the
`0x03` calls handed over at the very start, and it is not the name of any class
the title registers.

### The application's own classes

**`0x64`/`0x07` is where they arrive.** Its two arguments are a table and an
output array: the table is a count, a zero word, then that many class handles —
sixteen in one local title, nineteen in another — followed by the title's
string constants, and the array is where their objects go (below). These are
the application's classes, not the platform API surface `0x14` describes. What
`0x07` answers is a handle, which the module keeps and hands back to `0x0c` and
`0x06`.

A handle does not point at the start of its record. **The metadata block sits
`0x4c` bytes before the handle**, and the word at `handle + 8` points back at
it, which is what identifies the pair — every entry of both local titles agrees:

The header is set out under "Preparing a class" below. Past the handle the
record is either a vtable or a member list, and which one is the header's to
say.

**`super` is one of two things**, and which one is decided by where it points:
a `char *` to a name for a class the platform owns (`"java/lang/Object"`,
`"org/kwis/msp/lcdui/Card"`), and another entry's *handle* for a class the
application declares. One title's ninth class points at its sixth class's
handle, and the sixth's super is the `Card` name.

**Past the handle the record counts its own members, and that is where the
list ends:**

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

The owner word is the handle, which is what makes a member record recognisable
on its own, and `index` counts up from zero across the members of one class.
What confirms the counts is where the walk stops: stepping over `n` fields and
then `m` methods lands **exactly** on the next class's header, in record after
record. A wrong stride or a wrong count would land short or long, and it never
does. The names that come out are a class file's, unmistakably — `as:I`,
`at:Z`, `paint(Lorg/kwis/msp/lcdui/Graphics;)V`, `keyNotify(II)Z`,
`<init>()V` — under a superclass of `org/kwis/msp/lcdui/Card`. That is the
second confirmation, and it is the stronger one: a misread of a five-word
stride does not produce well-formed method descriptors by accident.

**Not every record has this shape, and `+0x0c` says which** — but what that
word is has a better answer than "a pointer into a record pool", and the answer
decides how much of the class the platform has to lay out. **It is the class's
vtable.** Where it points, the compiler built the vtable itself and put it in
the record; where it is zero, the compiler could not, and the platform builds
one. What separates the two is the superclass: a class rooted at
`java/lang/Object` has a layout the compiler knows end to end, and a class that
ultimately extends a platform class does not.

That is the same split as the member list. A record with a vtable carries no
member list at all — it does not need one, because nothing about it is the
platform's to decide. A record with a zero there carries the counted field and
method lists above, which is the platform's input for laying the class out.
Across the four local Java titles the counted shape covers four of nineteen,
six of sixteen, six of sixteen and two of eighteen classes — the substantial
ones, because the class that extends `Card` is the one the game is written in.

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

**So an object's first word is its vtable pointer, the word in front of the
vtable is the class handle, and slot *n* is at `vtable + 4 + 4n`.** The zeroes
in an inline vtable are the slots the class inherits from `java/lang/Object`
and does not override: the platform fills them, and `+0x24` says how many there
are — `vtableSize << 16 | inheritedSize`, with `inheritedSize` 11 for a class
rooted at `Object`. **Eleven is `java/lang/Object`'s vtable size in this ABI**,
and it is not negotiable: the compiler has already numbered every slot above it.

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

#### A class with no member list is entered through its interface table

A record that carries a vtable carries no members, and that is exactly the
record the platform has to enter when a title starts a thread on such a class:
`Thread.start` has to find a `run`, and there is no member record to find it in.
One local title's data branch stopped there — "t declares no run" — with the
class, its vtable and its code all present in the image.

What answers it is at `header + 0x14`, and it is the class's **interface
table**:

```
table + 0x00    count
table + 0x04    count pointers, one per entry
entry + 0x00    a char *name, or another record's handle
entry + 0x04    the vtable slot this interface's methods start at
```

The name-or-handle is the same convention `super` uses: a platform interface
arrives as a name and one of the application's own as a handle. That title's
thread class carries two entries — `java/lang/Runnable` at slot 10 and one of
its own interfaces at 0 — and slot 10 of its vtable holds a real code address,
so **`run` is a slot rather than a record**. Its own interface's zero is not a
slot: an interface that declares nothing has no methods to place, and only an
interface whose contract pins the order down can name a method this way.
`java/lang/Runnable` declares one method and nothing else, which is why it is
the one this platform resolves (`readJavaInterfaces`, `javaInterfaceMethod`).

Reading it is what took that branch from a stop to a run: the thread's `run`
entered, asked the handset for a property, compared the answer against a string
constant, dialled, and put the title's own "the connection dropped, reconnect?"
dialog on the screen — the offline path the title was written with.

`instanceSize` is the compiler's own count and it assumes the platform's
layout: a class with ten fields extending `Card` reports 23 words, so this
toolchain was built against a `Card` of 13. Nothing here has to honour that —
every field reference goes through an out array — but a platform that lays out
`Card` differently and then trusts the number allocates the wrong size.

**An object's data is a second block, at `object + 8`, indexed by word:**

```
ldr   r2, [r4, #8]          ; r4 is the object
ldrsh r3, [r5, #4]          ; r5 is the field out array, entry 2
ldr   r0, [r2, r3, lsl #2]  ; the field
```

A class handle is addressed the same way, and `[handle + 8]` is its header, so
"the data at `+8`" is one rule for both: an instance's fields, and a class's
own record.

### Preparing a class, and the calls that do it

The compiled code reaches a class by its handle — a `.data` address the
compiler baked in — and gates every use on the state halfword:

```
r4 = <class handle>
ldr   r3, [r4, #8]          ; the header
ldrh  r3, [r3, #0x1a]
cmp   r3, #3
blne  0x64/0x0b             ; not prepared: prepare it
ldr   r1, [<the 0x07 handle>]
bl    0x64/0x0c             ; then this, every time
```

So **`0x0b` takes a class handle and prepares the class** — which is where the
platform lays it out, builds its vtable and writes `3` into the state — and
**`0x0c` takes the class handle and the handle `0x07` answered** and hands back
whatever the code then uses as the class. Two more read the same
way from their call sites, and both are on failure paths: **`0x22` is entered
with a null receiver** after `cmp rN, #0` and **`0x23` with an index and a
bound** after a failed `blo`, so they are the null and the array-index throw.
They are the two most-referenced slots in a module — five thousand call sites
and three thousand — because every dereference and every array access has one.

**`0x0d` is the class initializer, not the cast failure.** Its call site is the
second half of the pair above and it takes two arguments:

```
r0 = <the class 0x0c answered>
ldr   r3, [r0, #8]
ldrh  r3, [r3, #0x10]
cmp   r3, #5
ldrne r1, =<a function in the module>
blne  0x64/0x0d             ; not initialised: run this
```

so `0x0d(class, clinit)` runs the class's static initialiser once and leaves
the class marked initialised. The earlier reading — a type-tag check that fell
through to a throw — was taken from the shape of the branch alone; reading the
same site with the function pointer in `r1` in view settles it, and `0x12`
below is the check that reading was looking for.

**The state the two calls test are two different words.** Preparation is
tested at `header + 0x1a` of the module's own record and answers 3;
initialisation is tested at `[class + 8] + 0x10` of whatever `0x0c` handed
back, and answers 5. That is the sharpest evidence that **`0x0c` does not
answer the module's handle**: `header + 0x10` of a module record is the
superclass word, and a platform that answered the handle would have the module
compare a superclass pointer against 5 and then write 5 over it.

What `0x0c` answers has the shape of an ordinary object of a platform class:
`[class + 8]` is the data block every object carries, its word 2 is the class's
name — call sites read `[[class + 8] + 8]` and pass it where a name is
expected — and its word 4 holds the state halfword.

### The string constants arrive with the classes

The `0x07` table is longer than its class list: a count, a zero word, that many
class handles, and then **a column of string constants**, each a `uint16`
length followed by that many UTF-16LE code units. The call's second argument is
where that column ends.

That is what an earlier reading of this table called "a column of name
pointers". The names in it were the constants of a Korean game, read a byte at
a time. The local titles carry 439 to 597 of them, and they read as a game's
own strings — item names, file paths, `"PHONEMODEL"`.

**The count the second argument declares is not the number of constants**: one
local title's array opens with 367 where the list has 489, another with 330
where the list has 439. What that array is for is still open, and it can stay
open: a literal-pool search for its address finds exactly one reference in the
whole module, the `0x07` call itself, so nothing the title runs reads it. The
constants themselves are read by `readJavaStringPool` in `java_class.go`.

**Where a constant's object goes is somewhere else, and the module says so
twice.** The first thing the module's Java startup does is `memset` the first
`4n` bytes of `.bss` — 439 words in one local title, 0x6dc bytes — and the
helper that materialises a constant reads that same array:

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

So **`0x09(group, units, length, slot)` builds one `java/lang/String` and
caches it**, and the cleared word is what makes the helper idempotent. The
`memset` length is an independent measure of how many constants there are: it
agrees with the pointer arithmetic above, which is worth having, because the
pointer arithmetic is the only other thing that says.

Reading the records is `readJavaClass` in
`internal/platform/lgt/java_class.go`, which refuses a handle whose
back-pointer does not name its header and stops rather than walking a count
that is not one.

The names are plain ASCII `char *`, not UTF-16 — an earlier reading of this
table said otherwise. The string constants are the opposite, and that is not a
contradiction: a name is the compiler's, a constant is the program's.

A failure anywhere past the Java table is reported as `ErrJavaAppUnsupported`
with the real stopping point kept inside it: the slot that stops the title is a
symptom, and the cause is that this is a Java app.

### The three thunks every class carries

`header + 0x2c` holds three addresses, and each is a small function in the
module that stands for one thing the compiled code cannot do inline. Reading
them is what turns the class record from a description into a protocol, because
between them they name every call a class costs:

```
body[2]   "give me this class"        prepare if [header+0x1a] != 3, then 0x0c
body[1]   "give me it, initialised"   body[2], then 0x0d if the state is not 5
body[0]   "declare my members"        looks the class up in the layout table
                                      below and calls 0x13 with what it finds
```

`body[1]` is what a call site reaches for. `new` is three calls in a row and
reads the same everywhere:

```
bl    <body[1] of the class>          ; r0 = the class, initialised
bl    0x64/0x0f                       ; r0 = a new instance
bl    <the constructor's own address> ; taken from the method record
```

so **`0x0f(class)` is the allocator** — 172 call sites in one local title, more
than any slot but the two throws. Nothing had caught a slot allocating before
because the allocation is not where a class is *named*: the name is spent one
call earlier, on `body[1]`.

`body[0]` is the one nothing in the module calls. Its only reference is the
class record itself, which makes it the platform's to call, and preparation is
the only moment that fits: `0x0b` is handed a class that has not declared its
members yet, and `body[0]` is how it declares them.

### The application's classes are laid out by a second class table

`body[0]` walks a table the module keeps in `.text` — a count, then 24-byte
entries `{ char *name, fields, staticFields, virtualMethods, methods,
staticMethods }`, each run a count in the high half and a start in the low half,
exactly as the platform class table's are. It finds the entry whose name
pointer matches the class's own and calls:

```
0x13(entry, class, fields, staticFields, virtualMethods, methods,
     staticMethods, fieldsOut, staticFieldsOut, virtualOut, methodsOut,
     staticMethodsOut)
```

which is `0x14` for one application class rather than the platform's thirty:
the same five member tables, the same five output arrays, one entry's runs.

**Only the classes whose layout depends on the platform are in it** — nine of
twenty in one local title, three of twenty-three in another, seven of
twenty-five in a third. A class rooted at `java/lang/Object` is absent, because
the compiler laid that one out itself and shipped its vtable inside the record.
Every class in the table has a platform class somewhere up its chain.

### The slot rules, and what they say about the platform's own classes

An entry's run gives the count; the class record's own header gives the base:

- **A field's slot is `instanceSize - fieldCount` plus its position in the
  run.** `instanceSize` is the compiler's own count at `header + 0x18`.
- **A virtual method's slot is `vtableSize - newMethodCount` plus its
  position**, where `vtableSize` is the high half of `header + 0x24` — but the
  run holds overrides as well as new methods, and an override takes its
  superclass's slot. Subtracting the whole run is right only when the class
  overrides nothing.

The first rule closes exactly, in every class of every local Java title: a
class with 52 fields whose superclass reports 25 words reports 77, and its run
starts where the superclass's instance ends. The second closes the same way for
most classes and comes up short by exactly the number of overrides for the
rest — one class in one title, five in another — so **an override has to be
found by name and descriptor against the superclass's own run** rather than
counted.

What can be checked is checked, and it is worth being exact about how much
that is. The first subclass of a platform class *defines* that class's size, so
its own arithmetic has nothing to disagree with; what it must not do is leave
the platform class's own declared methods above where the subclass starts, and
that is refused. From the second subclass on, the sizes have to agree with the
first — and every later class is checked against its superclass both ways.

The same arithmetic reads the **platform's** layout back out of the module,
which matters because nothing else says what it is. A class extending
`org/kwis/msp/lcdui/Card` with 11 fields reporting 24 words says Card is 13
words, and **all three local titles that extend it say 13**. Jlet is 6 words in
all three. The vtable sizes are not constants in the same way — Card is 25 in
two titles and 26 in a third, Jlet 21 and 20 — so a platform that hardcodes
them is wrong for some title, and one that derives them per module from the
subclasses is right for all of them.

An application class rooted at `java/lang/Object` needs none of this, and its
inline vtable pins one number the platform does not get to choose: those
records report `vtableSize` 11 higher than their own method count, so
**`java/lang/Object`'s vtable is 11 slots** and an application's own methods
start at 11. The zeroes in an inline vtable are the inherited slots, which the
platform fills when it prepares the class.

### What running one of these still takes

Both metadata calls are answered, the string constants are read, and the
protocol above is what the rest of the work is against.

The interface table is **implemented** as far as the local titles reach it:
`0x0b` prepares a class by calling its `body[0]` and building its vtable, `0x0c`
answers a class object, `0x0d` runs a `<clinit>` once, `0x0f` allocates, `0x09`
builds a string constant *and answers it*, `0x12` is the type check, `0x54`/
`0x55` are the frame markers a method enters and leaves through, `0x1f`-`0x25`
are the exception family, `0xe1`/`0xe2` are `String` and `String[]`, and the
four array calls are below.

What was left after that is **the platform API surface itself**: 30 classes and
99 static entries in one local title, most of them `org/kwis/msp/lcdui` drawing
calls this platform already serves for a Clet. They go into `java_api.go` one
measured method at a time — by name for a class the module lists virtual methods
for, and by slot for one it does not — and a method that is not there stops the
title with its own name. That is how the surface the running titles need was
filled in, and how the next one should be.

**A failure names where it came from.** Every one of these stubs returns through
`lr`, so a report carries the address in the module the call was made from, and
an unknown slot carries the classes of the arguments that turn out to be objects
this platform issued. Between them, one run says what a new slot is: the call
site says how many arguments were set and what happens to the answer, and the
argument classes say what the words are. Both are in java.go.

None of the remainder should be filled in by guessing: the failure that stops a
title today names its own cause, which is worth more than a title that starts
and then behaves wrongly. Every wrong reading in this section cost a pass to
undo, and each of them looked reasonable at the time.

### The two entries every class's run opens with

They answer with **the class**. Each platform class's static run begins with
two entries carrying no name and no descriptor, and what the module does with
the result settles them: one call site takes the answer straight to the
allocator, `0x0f`, and the next call constructs what came back. The other is
reached immediately before a class's first real static call, where the answer
is dropped.

So they are the platform-class half of the two thunks an application class
carries in its own record. A class the module declares has a record to reach it
by; a platform class has none, and these entries are the door instead. Reading
them as "initialise this class" and answering zero is what a first attempt did,
and the title then allocated an instance of nothing and failed several calls
later, at the allocation — a long way from the answer that caused it.

### Two kinds of virtual call, and only one of them is the platform's

A dispatch is `vtable[slot]`, and **where the slot number comes from is not the
same for every class**:

- For a class the module lists virtual methods for in its class table — the 29
  entries of one local title, which are `Graphics`'s drawing calls,
  `Display.pushCard`, `Card.repaint`, `Image.getGraphics`, `Jlet.notifyDestroyed`
  and `File`'s reads and writes — the slot is *answered by the platform*
  through the out array. Those can be turned back into a name here, and are
  dispatched by name like a static.
- For a class it lists none for — `java/lang/Runtime`, `String`, `StringBuffer`,
  `Thread`, `Random` — **the compiler baked the number**: the call site is a
  plain `ldr ip, [r3, #0x38]` against a platform header this implementation does
  not have. All that can be said about one is its class and its slot.

That the first kind covers every drawing call is what makes a screen reachable
without reconstructing the original platform's vtables. The second kind is a
piece of reverse engineering of its own, and the way into it is the call site:
how many arguments it sets, and what it does with the answer.

A platform class whose size nothing measures gets a **larger vtable than it
needs** rather than an exact one, because a slot past the end of the table is a
branch into whatever follows it. That is why an unimplemented method reports
`java/lang/Runtime vtable slot 13 of 64` instead of executing unmapped memory.

What a baked slot turned out to be, so far, and the evidence for each — the
table is `javaBakedVirtualSlots`, and a slot not in it is still reported by
class and number rather than guessed at:

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

#### The numbers are not arbitrary, and the rule is worth more than any one of them

Every row above was read off a call site, one at a time, over several passes.
Laid side by side they spell a rule:

> **A class's own methods begin at slot 10, in the order the library class
> declares them, and a method that overrides one keeps the slot the class above
> it gave that method.**

Nothing in the specification says so — the specification's listings are
alphabetical, which is a *different* order and produces different numbers for
`StringBuffer` and `Vector` than the ones the call sites gave. What says it is
that twenty-two independently-settled slots land on it exactly, across seven
classes and two inheritance chains, six of them in one consecutive run:

- `Object` 1/4/5 — `getClass`, `toString`, `notify` at declaration positions 1,
  4 and 5, which is what puts a subclass's first own method at 10;
- `InputStream` 10/11/12/13/14/15 — `read()`, `read([B)`, `read([BII)`, `skip`,
  `available`, `close`, the class's first six in order, with no gap;
- `DataInputStream` 25 — its own run starts where `InputStream`'s nine end, at
  19, and `readFully`, `readFully`, `skipBytes`, `readBoolean`, `readByte`,
  `readUnsignedByte`, `readShort` lands on 25. **That settles a question a call
  site could not**: the site only showed a 16-bit store, so `readShort` and
  `readUnsignedShort` were indistinguishable, and 26 is the unsigned one;
- `String` 10/28/33/34 and `StringBuffer` 10/13/18/22/23 — nine more, including
  `String.length` at 10, which had read as an oddity ("inside the eleven
  `Object` takes") and is simply the first own slot;
- `Vector` 15/29 — fourteen apart, which is exactly what this class's
  declaration order puts between `size` and `addElement`.

What it is for: **a call site plus the rule is a settled slot, where a call site
alone often is not.** `Vector` 19 sat unimplemented for a pass because
`indexOf`, `contains` and `removeElement` all take one reference and answer
something, and its one call site could not choose. The rule chooses — 18
`contains`, 19 `indexOf`, 30 `removeElement` — and the site then *agrees*: it
hands the answer to `new char[n][]` and `new int[n][3]`, a count of rows, which
`indexOf` answers and a boolean does not. Where the two disagree the site wins
and the disagreement is worth writing down; so far none has.

It also corrects one that had been placed by the specification's alphabetical
listing instead. `Runtime` 12 is `totalMemory`, not `freeMemory`: `gc` at 13 is
solid from its own sixty sites, and the declaration order behind it is `exit`,
`freeMemory`, `totalMemory`, `gc` at 10, 11, 12, 13. This platform answered
"what is left of the arena" at the slot that asks "how big is the arena", which
for a title sizing a cache off the heap is a different number. Both are
answered now.

**A slot a class inherits is reported against the class whose vtable first built
the stub**, not the class that declared the method: an `Object` slot reached
through a Jlet arrives named for the Jlet, and `InputStream.close` reached
through a `DataInputStream` arrives named for that. A number valid for a class
is valid for every class below it, so both lookups — the baked table and the
module's own numbering — walk up the chain, and `javaPlatformSupers` is what
they walk. That table is no longer only about slot placement; **it is also the
chain the catch test walks**, and a `catch (Exception e)` around a call that
throws `IOException` does not match while every class is rooted directly at
Object.

**Slots 1, 4 and 5 are `java/lang/Object`'s**, and the stub a call arrives
through is the one Object's own vtable was built with — so one entry answers for
every class that does not override it.

`Runtime` 11 and 12 are the pair no call site can settle. Nothing reads either:
the library holds exactly one site between them, in a module's startup sequence,
and the disassembly is unambiguous — the instruction after the call loads a
literal and branches to it without touching r0. So no title will ever say which
method it is, and the numbering above is the whole of the evidence: `exit`,
`freeMemory`, `totalMemory`, `gc` at 10, 11, 12, 13, anchored on the `gc` its
own sixty sites confirm. The same reading placed the WIPI C kernel block's slots
off `MC_knlExit`.

**Both answer a `long`, and that is a second thing the slot had wrong.** A
method whose descriptor ends in `J` puts the high word in r1 and the low in r0,
the way `currentTimeMillis` does. This one wrote only r0, so the top half of the
number was whatever the caller had left in r1 — a title comparing
`freeMemory() < n` would have compared against that. The arena is megabytes, so
the high word is zero; it now gets written as one.

### A type check is asked with a name or with a class

`instanceof` and the catch test come through one call, `0x12`, and its second
argument is **not one kind of thing**. Usually it is a name, read out of word 2
of a class object's data, which is where the array-type call reads one too. But
a title testing an element against an array type resolves the type first and
passes **the class object**: one site takes `firstElement()` off a vector, loads
the element's class out of its vtable, resolves `[C` through `0x0e` and asks the
check about the two — the same word in both registers, because the element *is*
a `char[]`.

Read as a name that word is no name at all, and the run stopped there. So the
second argument is looked up as a class this platform issued first, and only a
word that is no class is read as a name. Both forms reach the same walk up the
chain.

### What the running titles say now

**All four local Java titles run their own code, and three reach a play screen.**
They open their own resources, decode their own pictures, start their own game
thread and paint their own screens. Three go title screen → menu → new game → a
map with characters, an HUD and dialogue on it, driven from the keypad; the
fourth deadlocks itself on a save-backup check of its own when the network is
refused, which is the title's code rather than a gap here — see "A save-backup
check a refused connection leaves up for ever".

The third of them was the one whose menus stopped at `Vector` slot 19. Its
dialogue path builds a vector of the lines it is about to lay out, asks where
one of them sits, allocates `char[n][]` and `int[n][3]` off that answer, and
then drains the vector with `firstElement()`/`removeElementAt(0)` — four slots,
all four named by the numbering rule above rather than guessed at, plus the type
check that arrives with a class object. It now walks its world with dialogue
boxes rendering.

**Two of them were not stuck at all — they were waiting for a key.** Both open
with a connection the platform refuses, and both put the refusal in a dialog
with a confirm softkey and wait there. A run with no key script sits on that
dialog for ever, and it reads exactly like a title that has stopped. Pressing
the confirm key takes one of them through its notice screens into its menu, its
opening and its world; it takes the other three screens further, to the check it
really does stop on. **A probe run without a key script cannot tell a title that
has stopped from a title that is waiting**, and the cheapest way to tell them
apart is the key sweep in Testing below: the same prefix once per candidate key,
compared on the last frame.

What one of them does on the way: prepares its classes, runs its class
initialisers, enters `startApp`, builds a resource name out of string constants
and a `StringBuffer`, opens that resource out of its own archive, reads it whole,
decodes it into a surface, opens its save file, throws and catches the
`IOException` that comes back because the save is not there yet, loads its data
through a `DataInputStream`, constructs a `Thread` on itself, starts it, and
returns out of `startApp` with the game running on that thread. From there the
platform paints the card the title pushed, once a frame, and hands it keys.

The sections below are what it took, and each of the slots named in them was
read off a call site rather than guessed at.

### The frame loop is the platform's, because the title registers nothing

A Clet is called through the entry points it registers. A Java title registers
none: it builds a `Card`, pushes it on the display, and **the platform is what
calls that card's `paint` from then on**. So the loop lives here
(`java_frame.go`): once a tick, a `Graphics` over the screen is handed to the
card's own `paint`, and what it drew is presented.

- **A card paints only when it has asked to.** `repaint` is the request and the
  push is the first one. A title that does its work on its own thread and
  repaints at the end of it would otherwise be painted half way through an
  update.
- The rectangle a `repaint(x, y, w, h)` names is not honoured; the whole card is
  painted. Every local title paints its own background first, so the difference
  is work rather than pixels.
- `serviceRepaints` paints **now**, on the thread that called it, because that
  thread's stack is the live one. A title that drives its own frame from its
  game thread goes through there rather than through the tick.
- Keys arrive as `keyNotify(type, key)` on the card, with the specification's
  own `KEY_PRESSED`/`KEY_RELEASED` and the same key codes a Clet's events
  carry: one keypad serves both kinds of title.

The Graphics object is **the same one every frame**, reset before each paint: a
title that keeps the one it was handed — and the local ones keep it in a field —
has to keep one that still works.

### A Graphics that was never given an alpha still draws

`setAlpha` is the specification's three-way transparency: zero draws nothing,
255 draws normally, and **every other value counts as 255**, in as many words.
There is no blend to do, which is why this platform can serve it exactly.

The trap is the default. A Graphics starts opaque, and building its state from
Go's zero value makes a fresh one transparent — so a title that never calls
`setAlpha` draws a perfect frame into nothing. One local title did exactly that:
2,712 `drawImage` calls and 356 `fillRect` calls a run, and a black screen.

The rest of the drawing surface is the Clet's: one framebuffer table, one clip
and colour state, one glyph face. `drawImage` goes through the same blit, so a
picture's declared transparency is honoured on both paths; `setXORMode` draws
the difference into the same surface; and the anchor constants
(`HCENTER`, `RIGHT`, `BOTTOM`, `VCENTER`, `BASELINE`) move the point a call was
given to the corner the drawing code takes.

### A title's game loop is a thread, and a thread cannot be a call

A Clet is called and returns. A Java title starts a thread in `startApp` and
never comes back out of it, so **its guest stack has to survive across frames**
and the platform has to be able to stop it in the middle and resume it later.
That is a goroutine with a private ARM stack, the same arrangement the KTF
platform reached for the same reason: the session grants one step slice and
blocks until the thread parks — its budget spent, or a `sleep` it asked for — so
guest state is never touched by two goroutines at once, and a loop that never
returns still advances a slice a tick.

- `Thread.sleep` parks until the deadline **on the guest clock**, which is what
  makes a title run at the speed it was written for. The same call made where no
  guest thread is running has nothing to park and moves the clock instead.
- `Thread.start` is a baked vtable slot, and where it is called is what names
  it: on a `Thread(Runnable)` that was just constructed and stored into a field,
  past a null check, as the last thing `startApp` does before returning. It is
  the only reading under which `startApp` returning leaves the game running.
- A Java title registers no Clet, so the startup that insists on one lets it
  through: its initializer *is* its run.

### `synchronized` is two slots, and where they sit says which is which

A synchronized method compiles to `setjmp; if (exception) { A(this); rethrow }
B(this); body; leave the try region; A(this)`. **`B` runs where the protected
body begins and `A` where it ends and again on the way out of an exception**, so
`0x56` is monitorenter and `0x57` is monitorexit. The other reading — running
the entry on the way out — would leave the lock shut for good.

Threads here run one at a time, so a lock is only ever contended across a park.
Two things follow, and both are in `java_thread.go`:

- **A thread holding a monitor is granted another window instead of being
  parked**, which is what keeps a synchronized body indivisible under a
  scheduler that only switches at a park. A bound on the renewals keeps a loop
  inside one from holding the frame for ever.
- When the platform's own thread wants a lock a guest thread holds — the guest
  parked inside the body — the owner is **run from there until it lets go**,
  which is the wait a handset's own scheduler would have made.

A thread that ends gives back whatever it still held, so a failure inside a
synchronized body does not leave the lock shut.

### The slots the running titles settled

Each of these was read off its call site; the evidence for each is in the table
it is registered in (`javaBakedVirtualSlots` and `javaPlatformMethods`).

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

### An entry with no name can still be resolved by its descriptor

One local title hands over a static entry on `org/kwis/msf/io/Network` whose
name pointer is **null** and whose descriptor is `()I`, sitting between the
named `disconnect()V` and `<init>()V` of the same class. The class and the
descriptor are enough when they leave one answer: of the methods this platform
implements on that class, take those with that descriptor and drop every one
another entry of the same run already names. **One survivor is a resolution; two
are not**, and an ambiguous entry is still reported unnamed rather than guessed
at.

### An abstract method is a body, and it is not the one to run

`0x38` is what the compiler emits for a method with no implementation: the
body is a prologue, `mov r0, #0`, a call to `0x38`, and an epilogue, and the
records mark those methods `0x0400` — the JVM's own `ACC_ABSTRACT`.

Reaching one is a dispatch that went wrong, and the one here was this
platform's. **An override's slot is often not in the class's own run**: a run
holds the methods whose slot the module needs answering, and a class that only
overrides what its superclass declared needs none. One title's launcher subclass
has an empty virtual run and four overrides, two of them of abstract methods —
so filling a vtable from the class's own run alone left the superclass's
abstract stubs in place, and `startApp` called one. The slot for an override
comes from walking the superclass chain, not from the class's own answers.

### A class's statics live in its class object

`header + 0x48` is **how many words of static storage a class has**, and they
sit in its class object's data block after the five words the class object
itself uses: the state halfword the thunks read is at word 4, and a static is at
`data + 5 + slot`. Both halves of that are in the module. The compiler bakes the
offset where it can — `ldr r3, [r2, #0x700]` is word 448 of a class that
declares 448 static words — and asks for the rest through the static-field out
array, whose reads are `ldrsh` of a slot followed by `[block + slot*4 + 0x14]`.

Sizing the block by the class object's own five words is what **overwrote a
class object in two local titles**: a `putstatic` ran off the end of the block
and into whatever the arena handed out next, which happened to be the class
object built immediately after it. The symptom was a class resolving to a data
pointer of 160 several calls later, which is why the check that names it exists;
the cause was one number never read out of the record.

Every access in all three modules lands inside `5 .. 4 + count` — 253, 40 and
487 distinct words across them, with nothing outside — which is what says the
reading is the compiler's and not a coincidence of one title.

**What found it was a write watchpoint**, `Core.Watch`: the class object's data
word was watched from the moment it was built, and the first store to it named
the instruction, `str r7, [r3, #0x20]` at a known address, with the value it
wrote. Everything above followed from disassembling that one instruction. The
facility was already there for cheats; nothing had pointed it at the platform's
own structures.

### Both doors of a platform class answer with the class

The two unnamed entries every platform class's run opens with are the
platform-class half of the two thunks an application class carries, and
**neither of them allocates**. The first is reached before a class's first
static call and its answer dropped; the second is reached where the class itself
is wanted — handed to `0x0f`, or read for the name at word 2 of its data, which
is what the array-type call is given.

Answering the second with an instance is what a first reading did, and it
surfaces a long way off: `0x0e` is handed word 2 of an *instance's* data block,
which is zero, and reports an array of a class named at address zero.

### Arrays

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

The shift is what says how wide an element is, and the platform has to agree
with it. Four calls build and use one:

| Call | What it is |
|---|---|
| `0x0e(dimensions, class, code)` | the array type |
| `0x10(type, length)` | allocate |
| `0x11(type, &lengths, dimensions)` | allocate an array of arrays |
| `0xfa(array, index, value)` | store a reference |

`code` is the JVM's own `newarray` numbering — 8 byte, 9 short, 10 int — with
one of its own, 1, for an array of the class the second argument names. **The
class argument is only meaningful then**: two local titles pass zero for a
primitive and two pass whatever was in the register.

What told `0x10` from a cast — the other reading, since its answer is often
dispatched on — is that answering it with its second argument makes the title
read a field of the number 4 immediately. The length argument is a length: at
some call sites it is literal 3, 20, 50 or 62, and at others the answer to a
method call.

**A reference store is a call and a primitive store is not**, which is the
whole reason `0xfa` exists: the check a real runtime does there is the one that
answers `ArrayStoreException`, and it cannot be done from what the module hands
over — an array type carries its element's name, but an object carries no type
to compare against it. The store is bounds-checked and let through.

### Something writes over a class object

**This was a class's own statics, and it is fixed** — see "A class's statics
live in its class object". The check that named it is still there and still
worth keeping: nothing this platform hands the guest is out of the guest's
reach, and a class object is the one the guest touches most, so a stray write
there fails at the next resolve rather than where it happened. It costs one read
per resolve and names the class instead.

The wrong guesses are worth keeping too, because each cost a pass: **array
element widths** (allocating every array at eight bytes an element changes
nothing), **`0x10` being a cast** rather than an allocator, and **a platform
instance being too small** — that was a real defect, and it was not this one.
None of them would have been ruled out faster by more reasoning; the watchpoint
answered it in one run.

### Exceptions are `setjmp` and `longjmp`

A try region is two calls, and **the second one is not on the Java table at
all**. `0x1f` answers a buffer and the module hands that buffer straight to the
C library's `0x32`:

```
bl    0x64/0x1f            ; r0 = the buffer for this region
bl    0x1/0x32             ; setjmp: 0 now, the exception later
cmp   r0, #0
bne   <the handler>
...                        ; the try body
bl    0x64/0x20            ; leave the region
```

The pairing is what says so: 58, 57 and 63 call sites of `0x1f` in the three
local Java titles, and exactly 58, 57 and 63 of `0x32`, with the answer of the
first passed to the second at every one of the 178 sites. The handler reads the
thrown object's class out of `[r0]` and tests it against a catch type, which is
what makes the second return an object rather than a code.

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

`0x25` is read off its call site: it is called only when a divisor compares
equal to zero, and what follows it is the `0x80000000 / -1` special case.

`0x12` takes two arguments that are **not the same kind of thing**: the first is
the word in front of an object's vtable — a class record's handle for a class
the module declares, and the class object for one of the platform's — and the
second is a name, read out of word 2 of another class object's data. So it asks
"the class of the object I have, against the name the source wrote", and it is
answered by walking the chain. A platform class had a zero there until this
pass, because only a module-declared class has a record to be named by; its own
class object stands in now.

The jump itself is `Thread.SetContext`: the saved point is a whole guest
context, so the condition flags and the instruction set come back with the
registers. **A frame from an outer guest call cannot be jumped to** — restoring
registers moves the guest while this platform's Go stack stays where it is — so
a throw that would cross a platform call is reported with its class instead.

### Text, streams and pictures

What a String or a StringBuffer holds is kept **on this platform's side**, in a
map keyed by the object the module allocated. The module never reads a String's
own words — every use of one is a call, either a static entry or a vtable slot —
so there is no layout to agree with and nothing is gained by writing the
characters into guest memory. A StringBuffer is the same arrangement with a
different key, and `toString` copies it into a new String because a title that
keeps the answer and appends again must not see its copy change.

A `String` built from a `byte[]` is decoded as **EUC-KR**, which is what a
Korean handset's default encoding is and what the titles' own data files are
written in. Bytes that do not decode are kept as themselves rather than
replaced, which keeps a byte array of digits readable.

**The C side decodes the same way**, and it did not for a while. A Clet hands
`MC_grpDrawString` a buffer of handset bytes; rendering them as themselves puts
a codepoint-marked box on the screen for each half of each syllable, and one
title's entire notice screen — six lines of it — read that way. `MC_grpGetStringWidth`
decodes too, because it is measuring what `drawString` will draw.

**It is not applied to every string read out of guest memory**, only to the
ones that are text. A file name, a resource name and a property key are looked
up by their bytes on both sides of the platform, so decoding one would turn a
match into a miss: `readCString` stays bytes and `readCText` is the decoding
reader beside it.

**This is how a title reads its own data**: `getClass()`, then
`getResourceAsStream(name)` on the answer, then `new byte[available()]`, then
`read`, then `Image.createImage` on the bytes — or a `DataInputStream` over the
same stream when what it wants is numbers. A wrapper and the stream under it are
one open stream here, which is what makes closing both of them work: the second
close is a no-op, the way the language defines it. The resource comes out of the archive's own
files, the same place a Clet's resource read takes it from, and the picture goes
through the same decoder into the same framebuffer table — so a Java title and a
Clet see one filesystem and one kind of surface. A resource that is not in the
archive answers null, which is what the specification says and what every call
site expects: each one null-checks the answer before touching it.

### A title's save is the filesystem a Clet writes into

`org/kwis/msp/io/File` goes to the same store, the same open handles and the
same paths as `MC_fsOpen` — one filesystem, so a Java title and a Clet of the
same game would see each other's files. Its virtual methods are in the module's
own class table, so they arrive by name: `sizeOf`, the `read` and `write` pairs,
`write(int)` and `close`.

**Its mode constants are not the C ones.** The Java class numbers them 1, 2, 3,
4 — read-only, write, write-and-truncate, read-write — where `MC_fsOpen` uses
1, 2, 4, 8 for the same four, so passing one straight through turns a title's
read-write open into a truncate. They are translated.

Opening a file that is not there for reading **throws**, which is what the
specification says the constructor does and what the original runtime does, and
it is the first thing a title does with its save: the `IOException` on a fresh
profile is an ordinary part of a first run, and the title's own handler is what
creates the save. That path is only reachable because the exception machinery
above works — and it was the first thing to exercise it end to end.

**A file a Java title writes is read back on the next run**, which is as far as
the round trip has been carried by a title rather than a test. One RPG writes a
ten-byte file of its own during play; started again against that tree it opens
the file, reads all ten bytes and closes it, twice, before it paints anything —
`-trace-live io/File` is the whole of the evidence and it takes one run. What is
*not* shown is a progress save: that title's "continue" still answers "there is
no saved data", because nothing has reached the save its own menus offer.

**How far the driving got, so the next attempt starts there.** The title is
scripted from boot to its world: confirm the refused connection, press through
the notices, the title menu and the opening dialogue, then the tutorial battle,
which is the part that needs reading rather than a key every forty ticks. Its
tutorial asks for particular keys — `*` for a monster's information, `CLR` to
come back, `#` to attempt a capture — and **a key while a message box is up only
takes the box down**, so each instruction takes two presses, one to dismiss and
one to act. Alternating `fire` with the asked-for key is what got through it;
`fire` alone sits on the same message for a thousand ticks and reads exactly
like a title that has stopped.

Past the battle the world opens and its own screens are keyed: `*` is the quest
list, `soft1` the party status, `#` the map, `fire` takes the selected entry and
`clear` leaves it, and `0`, `1`, `2`, `3`, `call`, `hangup` and `7` do nothing in
the field. The save the module carries strings for — a menu of "main menu / help
/ settings / **save**", and `master.sav` beside the ten-byte `mastercom.sav` it
already writes — is on none of those, so it is either behind a key not yet swept
(`soft2`, `4`, `5`, `6`, `8`, `9`) or behind an item, a place or a person in the
world rather than a menu.

**This generalises, and it is a static read.** Several LGT archives ship their
help text as a resource, and it can be extracted without running anything:
`data/StrHelp.*` inside the inner JAR. The `.zt1` form is an eight-byte header —
two little-endian lengths, compressed then uncompressed — followed by a zlib
stream, and the text inside is EUC-KR with `!cRRGGBB` colour escapes and `!N`
for a line break.

What comes out is the title's own answer to "which key does this". Two titles of
one family were read this way while a QA pass was stuck sweeping menus blind:
one lists **quick save on the send key** and no save menu at all, which is why
menu sweeping could never have found it; the sibling lists no save key
whatsoever, so its save is a menu entry or automatic and the send key is the
wrong thing to press. Two archives in the same set carry no help resource, which
is itself worth knowing before budgeting a pass on them.

Reading the help before scripting a route costs nothing and replaces the
expensive half of a QA pass. Where the file exists, it is the cheapest evidence
in the project.

**What the help does not tell you is how far away the key is.** It answers
"which key saves", not "how many screens of forced introduction stand between
the title screen and a character who can press it". One title read this way
named its save key, and a route driven fifteen thousand ticks and a dozen key
presses deep was still inside a scripted conversation, where the same key
advances dialogue instead. A key map makes the last step cheap and says nothing
about the ones before it, so it shortens a route rather than replacing one.

**The other half of the answer is in the module, and it is just as cheap.**
`strings` over `binary.mod` finds the filenames a title writes: the same title
carries `option.sav` — the settings file that appears immediately, and which is
therefore *not* evidence that saving works — and `Save%d.dat`, a printf format,
which says the progress save is **slot-numbered** rather than a single file.
That is worth knowing before a run: it names exactly what to look for in the
save tree, and its absence beside a present `option.sav` distinguishes "the
title has not saved yet" from "the platform did not write it".

**The title's own help screen is the key map, and reading it beats sweeping.**
`txt/strHelpDesc.dat` in the archive lists all three of its screens: in the
field, `#` is the minimap, `*` the quest list, the clear key is the menu and the
dialogue skip, and **the send key is a quick save**. In a menu the decision key
is "EZ" or `5`; in battle those attack, `*` shows information and `#` attempts a
capture. Two things follow. The save is not a menu entry at all, which is why
sweeping the menus never found it — the module's "main menu / help / settings /
save" strings are a different screen's. And "EZ" is `MH_KEY_SOFT3`, the third
soft key, which this runner's key table did not have; the title offers `5` for
it everywhere, so nothing here depended on the gap, but a title that does not
offer the alternative would have looked like a title that ignores its own
prompt.

What that has not produced yet is the file. The send key reaches the card's
`keyNotify` — eighteen deliveries in one run, by the log the frame loop writes
for every key — and no `master.sav` appears beside the ten-byte `mastercom.sav`.
So the key is not the missing piece any more: whatever the title checks before
it writes is, and the module's own use of that filename is where that starts.

**The route there is a script and it replays**, which is what makes the next
attempt cheap: one cycle of `fire`, `*`, `fire`, `clear`, `fire`, `#`, `fire`,
`soft1` every sixty ticks, held fifteen, carries the title from its first notice
through the menus, the opening, the tutorial battle and into the village with
its quest list open, in about seven thousand ticks. The alternation is the point
— a key while a message box is up only takes the box down — and a sweep of the
remaining keys is the same script with a different tail.

**`runlgt` now takes `-route`**, the same screen-driven script `runktf` has
(see [`cli.md`](cli.md)), which is what a title this long needs: an eleven
thousand tick drive written as absolute `-key` ticks lands somewhere different
every time it is run, and `wait-idle`/`wait-change` are how a step waits for
the screen instead of for a number.

**Two dead ends, so the next attempt does not repeat them.** The send key's
code is right: `MH_KEY_SEND = -10` is the specification's, and the card's
`keyNotify` takes the same codes a Clet's event does. And the module *does*
carry `master.sav` — it is one of the UTF-16 constants at `0xe66cc`, beside
`mastercom.sav` — but naming it proves nothing: with `-trace-live` reporting
every distinct string constant the title builds (below), `master.sav` appears
in a run of two dozen constants built together, so it is a class's static
table, not the save path running.

What that leaves is a state question. The strings around it are one screen's
menu — 선물하기 / 순위보기 / 게임문의 / CREDIT / 이용정보 / **저장하기** /
환경설정 / 도움말 / 메인메뉴 — so the save entry and the save filename are the
same class's, and the help screen's "quick save on the send key" is a second
door into the same code. Neither has been reached: the field the send key was
pressed in still had a quest box up, and the clear key in that state opens the
quest message rather than the menu.

**The save filename is a module-wide constant, not a class's own — the earlier
reading of it was wrong.** Reading the module settles what the string is
attached to, and it is attached to nothing: the constants are UTF-16, each
one a `uint16` length followed by its characters, living in `.text`, and
`.data` carries a **single global table of pointers to them** based at
`0x140019c`. `master.sav` is constant **index 0** and `mastercom.sav` index 1;
what follows them is `수`, `화`, `목`, `풍`, `광`, `무`, then `unit_frm`,
`IMG_NUM`, `.mbm`, `/img/`, `PHONENUMBER`, map and NPC names. So the run of two
dozen constants around it is the module's own pool in whatever order the
compiler emitted it, and **adjacency says nothing about which class uses a
string**. The inference that the save entry and the save filename belong to the
same class does not follow from it, and the menu strings that suggested it are
somewhere else in the same table.

What the module does say is how a constant is reached. The function at
`0x1834` is the interner:

```
intern(index):
    r1 = [0x140019c + index*4]      ; the constant's address
    r2 = [r1]                       ; its length
    r1 = r1 + 2                     ; its characters
    r3 = 0x1500000 + index*4        ; the .bss slot that memoises the String
    call [0x14046ac](r0, r1, r2, r3)
```

**Nothing reaches it with a `bl`** — a scan of every branch-and-link in
`.text` finds no caller. This module calls its own helpers the way it calls the
platform's, through the literal pool:

```
ldr r3, [pc, #..]     ; the helper's address, from this function's pool
mov r0, #<index>      ; the argument — after the load, before the call
mov lr, pc
bx  r3
```

Two traps in that shape, both of which produced a confident wrong answer here
before they were caught. **A "who calls X" search has to look for the pool
entry, not for `bl`**: 161 words in `.text` hold this helper's address, and a
branch scan finds none of them. And **the argument is set between the load and
the call**, so a scan that takes the nearest *preceding* `mov r0, #imm` reads
some earlier unrelated value — that reading named two call sites for constant
zero, and disassembling them showed the real index was `0xd3`, set on the
instruction after the load.

Done correctly there are 415 intern call sites, 227 of them with a literal
index and 188 computing one. **None of the literal ones asks for constant 0**,
so the save filename is fetched with a computed index — which is why the next
step is not another search for the constant. It is the file API: the module's
import stubs give the whole platform surface a title uses without running it,
and that is where an open-and-write is.

### Every file a Java title opens, from the platform method table

The chase for the save filename through the constant pool was the wrong end of
it. The name is not interned at the call site at all — it comes out of a helper
chain (`0xdb520` into `0xdb57c`) that dispatches virtually, which is why no
literal intern index for the constant exists to find.

The right end is the **platform method table**. `javaSVCLoadClasses` fills a
`.bss` table — `0x1500820` in this title — and every platform call is
`ldr ip,[table,#offset]; mov lr,pc; bx ip`. So the offset *is* the method:
`0x54` is `File.<init>`, and finding every file the title can open is one scan
for functions that load the table and then load offset `0x54`. There are
**eight** in a megabyte of module:

| function | what |
|---|---|
| `0x169c`, `0x173c` | runtime glue, near the module's entry |
| `0x1e1c8` | |
| `0x79ba0`, `0x7ac84` | a pair, the shape a save and a load take |
| `0xa523c` | |
| `0xda95c` | writes the packaged settings file |
| `0xdad28` | reads it |

The last two are confirmed by running: the debug log names the call site of
every platform call, so driving the title and reading `from=` addresses is what
identified them in one run rather than in a static search. **`from=` is the
return address**, so the call is the instruction before it.

That turned the remaining question from "where in a megabyte" into "which of
six functions", and the adjacent pair answered it. **`0x79ba0` is the save and
`0x7ac84` is the load**, and the modes say so on their own: the first opens
with `mov r2, #4` — write-and-truncate — and the second with `1`, read-only,
which are the two halves of a round trip.

What they open is the part worth keeping. Neither names a file: both take it
out of an **array element**, `ldr r1, [r0, #4]`, behind an array bounds check
against the length at `[r0]`. So this title chooses its file by *index*, which
is what a save with slots looks like from underneath — and it is why chasing
the filename through the constant pool never found a caller. The array is
filled with computed indexes rather than literal ones, which is consistent with
a loop over the constant table and consistent with the 188 intern sites whose
index is not an immediate.

`master.sav` being constant 0 and `mastercom.sav` constant 1 makes element 0
the progress save and element 1 the settings the title already round-trips.
**That last step is inference, not evidence**: the array's construction was not
traced, only the shape of the two functions that read it.

What this settles for the platform: there is nothing left to implement here.
The write path is proven end to end by the settings file, which this title
opens, writes ten bytes to, closes, and reads back on the next run. Reaching
the save is a driving problem, not a platform gap.

**And it is not in a menu.** Driven into play — the title screen's second entry
loads the packaged save and walks straight into the village — the in-game menu
opens on `soft1` and holds five entries: continue, how to play, settings, main
menu, quit. **There is no save among them.** Leaving to the main menu was
driven, and so was quitting, and neither writes a byte; the menu's own footer
offers `EZ:SELECT` while `soft1` is what opens it, which is the kind of detail
that costs a run to find. So whatever calls `0x79ba0` is a story or a place
rather than a command the player gives.

The module says how many candidates there are without another run. The loader
applies no relocation, so a function's address appears in the callers' literal
pools as itself: `0x79ba0` occurs **five** times in the module and `0x7ac84`
once. One of each sits in a table at `0xf7058`/`0xf7074`, twenty-eight bytes
apart — a method table rather than a call — which leaves **four literal-pool
sites for the save and none for the load**. Disassembling those four is the
next step, and it is a static one.

**A second Java title answers the same question out loud, and it is not this
one.** Driven to its first playable screen, *the other* Java title's in-game
save answers `저장할수 없는 지역입니다!` — "this is not an area you can save
in" — which is the game's own rule about where the player is standing rather
than anything the platform did. Three things from that run are worth keeping,
because they are what a run of either title costs:

- The save key is `soft3`, the EZ key, and `call` produces the same message:
  that title maps both to one action.
- The wall in front of it is the opening cutscene. It is very long, `skip:#`
  does **not** skip it, and only repeated `fire` walks it — about 6,600 ticks
  of route before the player has control at all. A key sweep before that point
  sees nothing but dialogue and reads as "no key does anything", which is how
  an earlier sweep concluded exactly that.
- Walking out of the opening room into the next area does not change the
  answer, so the whole opening dungeon is a no-save region and reaching a save
  point there is a play problem rather than a driving trick.

That title also writes its own files while being played — `LG_FN0`, five bytes
of settings, and `LG_FN4`, `LG_FN5`, `LG_FN6`, eleven zero bytes each, which is
three empty slots. So the file path this section proves for one Java title is
exercised by a second one, in-game, without either of them having saved
progress yet.

### A file packaged beside the JAR is a file the title can open

The archive is a zip holding `app_info` and the game's JAR, and only the JAR's
entries were being offered to the guest. A handset stored the whole download in
the title's own directory, so anything packaged beside the JAR is a file the
title can open by name — and three local archives ship one:

| what | archive |
|---|---|
| `mastercom.sav`, ten bytes | one Java title, and its coin-edited redistribution |
| `certification` under a Korean directory name | one title |
| `P/kickass`, thirty kilobytes | one title |

The Java title is the one that proves it. It asks for its packaged save
read-only at startup, was refused, and **wrote a fresh empty one over it** —
so the ten bytes the archive shipped, which hold `0x000f4240`, never reached
the game. With the packaged files offered it opens the file it came with, which
is what the handset did.

The JAR wins a name collision, because the JAR is the application and a file
beside it is data the download happened to carry. The JAR itself and `app_info`
are not offered at all: the loader has already read both, and answering a game
with a megabyte of JAR would be a surprise rather than a resource.

The other two archives do not ask for theirs in an early run, so what this
changes for them is unmeasured — the certificate is the interesting one,
because a title that checks one and cannot find it is the shape one KTF title
needed `provision` for.

### What a Java title imports, from the stubs alone

The stub scan the section above describes works on the Java titles too, and on
this one it answers 126 stubs across six tables:

| table | slots | what |
|---|---|---|
| `0x1` | 88 | the C library, plus `0x32`/`0x33` — the OEM framebuffer pointer and width |
| `0x64` | 33 | the Java interface |
| `0x1f8` | 2 | LGT's own, `0x16` and `0x17` |
| `0x1fc`, `0x1ff`, `0x201` | 1 each | slot `0x3` of three tables nothing else names |

That a Java title still imports the OEM framebuffer accessors and eighty-eight
C library functions is worth knowing before reading its Java calls as the whole
story. The three single-slot tables are new — no Clet here resolves them — and
each is reached at index 3.

Two smaller things fell out. The archive ships `mastercom.sav` itself, ten
bytes of `02 00 1e 01 00 0f 42 40 01 01` — `0x000f4240` is 1,000,000, which is
what the coin-edited redistributions of this title differ in, so that file is
the account and `master.sav` is the progress. And a literal-pool scan over a
module like this has to disassemble each function from its own prologue: linear
disassembly of the whole `.text` loses sync on the constant pools between
functions and then reports the loads it never decoded as absent.

**The key that takes a box down is `5`, and it was not the one being pressed.**
The route above alternates `fire` with the key an instruction asks for, on the
reading that any key dismisses a box. It does not: the boxes print `EZ:NEXT`
and `EZ:SELECT` in their own corner, `EZ` is `MH_KEY_SOFT3`, and the title
offers `5` for it — so `5` takes the box down and advances the tutorial, and
`fire` does nothing at all. Four keys were measured against that state and
**every one of them left the frame bit-identical**: `clear`, `call`, `soft2`
and `4`. A screen that answers exactly one key and ignores four reads like a
title that has stopped, which is what the earlier sweep was reading.

What that buys and what it does not: pressing `5` walks the tutorial — the item
box, the dialogue behind it, the party lesson, and the party screen answering
`파티넣기` — but the tutorial is a **gate**, not a prompt. It holds each screen
until its own instruction is followed, so `CLR:BACK` does not leave the party
screen while the lesson is unfinished, and the field the send key needs is
behind however many lessons remain. Three routes of eighteen, twenty and thirty
keys each got further into it and none of them came out. The next attempt is
that same route with a tail that follows the instructions rather than pressing
past them — and it is worth reading the module first, because that is what
settled the same question on the other platform in one pass rather than in
four eight-minute runs.

### What a title has named

`takeJavaStringConstant` reports each **distinct** string constant the title
builds, once, at debug level (`LGT java names`). A constant is built by the
code that holds it, so the set answers "has the title reached the code that
names this" without a disassembly — which file it is about to open, which
screen it is about to draw. Only the first build of each is reported: one
title rebuilds the same constant every frame, and a line per build buries
every other answer.

Its limit is the one above: a class whose constants are all built together in
its static initializer says nothing about which of its methods ran. A name
appearing is a floor, not a proof.

### A caught throw is not a return

The platform method that throws has no result to give, and **whatever it would
have answered must not be written**: the jump has already put the exception in
the answer register and the whole context is the handler's. Writing a result
over it made a caught failure read as a success — the title took the *normal*
path out of its `try`, believing a file it had never opened was open, and failed
two hundred instructions later. The throw travels back as a sentinel error for
that reason, and every caller that services a platform call knows it.

### The String pair, and the two slots that answer them

`0xe1` and `0xe2` take **no argument at any of their 254 call sites**, and each
answer goes straight to one call: `0xe1`'s to the allocator, followed by one of
`java/lang/String`'s own constructors, and `0xe2`'s to the array allocation,
whose elements are then stored through the reference store `0xfa` and read back
out of fields declared `[Ljava/lang/String;`. They are `String` and `String[]`,
which is what a compiler would hard-wire: a Jlet is entered with one and every
string constant is the other.

`0x9` **answers the string it makes**, and the cache slot it is passed is its
own to check. The module's helper for a constant is nothing but this call — look
the pool entry up, pass the units, the length and the slot, return what came
back — so answering zero makes every constant null. Three of them appended into
a `StringBuffer` is how it showed: a resource name that came out as
`"nullnullnull"`, four calls away from the constant that was wrong.

### What `"Game"` names, and why it is not in the class list

It is a class. Every local Java title has a class record whose name is exactly
the module's `argv[0]` — `"Game"` in two of them, a two-letter name in the
third — and that class extends a class that extends `org/kwis/msp/lcdui/Jlet`.
So `Main.main({"Game", "", "true", "true"})` is the launcher being asked to
instantiate the named Jlet and start it, which is what the earlier reading
could not see.

What hid it is that **the launcher class is not in the list `0x07` hands
over**, and neither are the two abstract classes between it and `Jlet`. The
list holds the classes the module itself resolves at run time; a class the
module never writes `new` for has no reason to be there, and the launcher class
is precisely the one the *platform* constructs. Reading the list as the whole
of the application is what made the class look absent.

They are all there in the image, laid end to end in `.data`, and a record is
recognisable on its own: the word at `handle + 8` names its header, and the
header ends with the `0xfffffffe` sentinel. A scan for that pair finds 20, 23
and 25 records in the three local titles with no false positives, which is how
`findJavaClassRecord` finds a class the module never handed it. Every local
Java archive now reports its own launcher class, its superclass chain and the
address of the `startApp` it would enter, out of a real run rather than out of
the ELF.

The scan does not skip the executable sections, and that is not thoroughness:
**a module marks the section its class records live in executable**, so
skipping by that flag finds nothing at all. The two words a record is
recognised by are specific enough that walking a megabyte of ARM finds no
record that is not one.

The Jlet subclass carries `startApp([Ljava/lang/String;)V`, `pauseApp`,
`resumeApp`, `destroyApp` and `run()V` as ordinary method records with their
own code addresses, so **once the object exists, starting the application is a
call to an address the record already holds**. `JletWrapper`'s entries in the
static-method table are the other direction — the platform's side of the same
lifecycle — which is why no call site for them was ever found.

`runJavaLauncher` does exactly that: prepare the class, allocate an instance,
call `<init>`, call `startApp`. The `String[]` it should be started with is
passed as null, because nothing builds an array yet; a title that reads it will
say so through the null throw, which is a better place to find out than a
guessed array. Nothing pauses, resumes or destroys the application yet, and
nothing drives the event queue a Jlet expects to run on.

**Calling guest code from a platform call has one rule**, and everything above
depends on it: it runs on the thread that is *running*, not the platform's own.
See `callOn`, and the paragraph under "The slot that runs a function" for what
ignoring it cost.

A second rule came out of the launcher: **a class's object is built before its
layout is**. Laying a class out runs the module's own code, and that code
resolves classes — including, on the way through its own members, the one being
laid out. Answering that re-entry with a half-built class hands back a null,
and the null surfaces at an allocation several calls later.

### Reading a module without running it

The findings above came out of the ELF rather than a run, because the metadata
is all in `.data` and `.text` at the addresses the module will use. What that
takes: parse the section table, resolve an address to a file offset, and
disassemble with `capstone` in ARM mode — the AOT bodies and the startup glue
are ARM, not the Thumb the entry point is in.

Three things make it fast. The module's import stubs are a run of four-word
records in `.data`, each `str lr,[sp,#-4]!; bl <resolver>; <table>; <index>`
before they resolve, so **the complete set of platform functions a title uses
falls out of a scan for that first word** — 41 slots in one local title, and
which stub is which is the two words after it.

A literal pool search finds the call sites: build one index of every
`ldr rN, [pc, #imm]` in `.text` against the word it loads, and it answers both
directions at once. Asked for a stub's address it gives every call site of that
platform slot — which is how `0x0f`'s 172 allocations and `0x22`'s 4083 null
checks were told apart from `0x11`'s four — and asked for a `.bss` address it
gives every use of an out array, which is how each array's element size and
meaning was settled. **The counts alone rank the work**: a slot with thousands
of sites is on every dereference, and one with a single site is startup.

The third is that the layout tables can be found rather than followed. A class
record announces itself (`[handle + 8] == handle - 0x4c`, and `0xfffffffe` at
`header + 0x44`), and the application's layout table is the only run of 24-byte
entries whose first words are all names of those records. Neither needs the
module to have run.

## Deliberately incomplete

- **LGT Java apps play, and the platform API behind them is partial.** The
  class protocol, arrays, exceptions, monitors, threads, text, resource streams
  and the drawing surface are implemented. What is not implemented is whatever a
  title has not asked for yet: a method that is missing stops the title with its
  own name rather than a wrong answer, which is what makes the next one cheap to
  add. See "What the running titles say now". The reference implementation stops
  before any of this.

  Where each stands, and what each is waiting on:

  - three reach a play screen and are driven from the keypad. **A route that
    reaches a play screen is not a route that covers the menus**: the branch of
    one title's menus that stopped at `Vector` slot 19 was found by a denser key
    script than the one that plays, and the four Vector slots and the type-check
    argument behind it are now taken;
  - one stops on a save-backup check of its own, below.
  - the branch of one title's menus that offers to send its data somewhere now
    runs its thread and reaches the title's own offline dialog — "the
    connection dropped, reconnect?" — which is a missing server rather than a
    gap here. Getting there took the interface table below, the handset
    property and `String.equals`.

- **A save-backup check a refused connection leaves up for ever.** The fourth
  Java title paints "checking the save backup" once and never repaints. It is
  not a missing API — nothing reports one — and it is not the save file: the
  file it opens read-only just before is absent and the `IOException` is
  *caught*, and a second run against the tree the first one wrote stops in the
  same place. **Every key was swept and every run ends on the same frame**, so
  it is not waiting for one either. It is the title's own online gate, and the
  whole of it reads off four methods of one class:

  - the card class every screen of this title extends declares `paint`, and
    every screen inherits it. It draws a notice while a static word is set, and
    when that word is zero it calls a private helper of its own whose last
    instruction clears an instance flag;
  - `while (flag != 0) Thread.sleep(10)` opens the method the platform's frame
    loop calls to run a screen — so a screen does not start until it has been
    painted once **without a notice up**;
  - the save-backup routine raises the notice, constructs
    `org/kwis/msf/io/Network` and calls `connect`. On the -1 a refused
    connection answers with, it branches straight to its own epilogue with the
    result 2, **over the call that takes the notice down**;
  - the menu passes that 2 to the method that pushes the next screen. The
    notice is still up, so that screen's loop waits for a paint that cannot
    happen.

  So nothing here is waiting for the platform: the title deadlocks itself when
  the network is refused, with no key, no timeout and no second exit. **A false
  yes does not rescue it either**, and that is worth knowing before anyone
  reaches for one: answering `connect` with 0 — the specification's "access is
  available" — takes the title off this screen and stops it nine ticks into the
  run instead, at `org/kwis/msf/io/URL.find`, which is the next thing it asks
  for and is not implemented. The refusal is the answer that gets the title
  furthest, and where it stops with it is the title's own decision.

  Two earlier readings of the same screen were wrong in a way worth keeping.
  The class whose paint clears the flag is not a sibling of the card on the
  display — it is the **superclass of every card in the title**, so its paint
  runs on every frame; and the store that clears the flag is the tail of
  `b(Lorg/kwis/msp/lcdui/Graphics;)V`, the method **before**
  `a(Lorg/kwis/msp/lcdui/Graphics;)V` in the record. Both came of reading a
  member list without the class hierarchy beside it. What settled it in one run
  was the backtrace under "Where a title is waiting" below.
- **No input automaton.** The four input-method calls that describe and select
  a mode are answered as the specification defines them; `MC_imHandleInput`
  composes nothing, so a widget that types through the platform stays empty.
  See "Character input" for why a guessed keypad layout is worse than the gap.
- **A scene load takes seconds, and the screen holds its loading art for all
  of it.** Taken again after the page-permission change in `docs/armcore.md`,
  on an M-series desktop, release profile: the worst single tick across the
  local archives is **6.4 s** (best of five; the same run repeated lands
  between 6.4 and 7.1 s, and the debug profile measures the same, which is
  what a cost that is all guest ARM should do). It belongs to a chapter-based
  title entering its first scene. The RPG world entries are smaller: 2.6 s for
  the heaviest of them, 1.2 s for the one after it, and most titles never take
  a tick over half a second.

  The way to take it is `runlgt -ticks N -key ...` with a key script that
  reaches the load, then `slowest_tick_ms` from the JSON summary — an average
  hides a load that happens inside one timer callback. The number moves by a
  tenth between runs on a warm machine, so it is worth taking a best of
  several rather than one.

  The budget is not the limit — the load retires roughly 6e8 instructions
  against a 3e9 ceiling — so this is interpreter throughput and nothing
  narrower. `docs/armcore.md` carries what was measured about that and what was
  tried and abandoned.

  The figure that stood here before was "about ten seconds", measured before
  the page-permission change. It is not a like-for-like predecessor of the 6.4
  s: what it timed was recorded as a world load rather than as a title and a
  scene, so how much of the difference is the change and how much is a
  different load is not something this measurement can say.
- **No network.** The block reports failure: a game's own state machine handles
  that, while claiming a connection would make it wait for data that never
  arrives. It is the *whole* block rather than only the connect call, because a
  title that is refused a connection still tears down what it had started, and
  stopping it at the teardown turns a handled refusal into a crash.
  `MC_netClose` returns void, so it reports success.

  `MC_netConnect` is the exception, and it reports failure through its callback
  rather than its return value, because the specification says an accepted dial
  is answered that way and a refused one is never called back at all. Answering
  only the return value left a title that waits on the callback waiting
  forever. See `wipic_net.go` and `docs/network.md`.

  An online-gated title with no offline path still cannot be played — that is a
  missing server, not a wrong stub. What it does about it is now its own
  decision rather than a stall: one asks whether to authenticate and takes
  "no", another reports the failure and continues to its title screen, and a
  third shows its anti-piracy notice and exits from inside its own callback.

  **The third one's exit is the game's, and the call ring says so in one line.**
  Its last three platform calls are the module resolving import table `0x1fb`
  entry `0x68` and then calling it: `MC_knlExit`, from an address in the title's
  own code. A title that resolves the exit slot lazily at the moment it wants to
  quit is a title deciding to quit. There is nothing for the emulator to do
  about that short of a server, and answering the connection differently would
  be answering it falsely. This is the shape of evidence worth taking before
  filing an online-gated title as a defect: `-trace N` and the last line.
- **Animated images.** `MC_grpDecodeNextImage` reports that an image is whole
  as soon as it is created, because nothing here decodes an animated encoding
  and there would be no frame clock to run it against. The same reason the
  other WIPI platform gives.
- **The tests do not load a real archive.** No LGT archive is in this
  repository, so they run against a newly authored ARM module
  (`fixture_test.go`) that resolves its imports, registers a Clet, writes the
  framebuffer directly and flushes, plus slot-level tests that drive one call at
  a time. That is enough to pin a contract once it is known and to keep it from
  drifting, and it is not enough to *find* one: every contract corrected above
  was found by running a real title and reading what it asked for.
- **The restart notice is dismissed by the key it offers, and the run it ends
  is its own.** One title boots, paints, and draws "the game downloaded
  correctly, restart it to secure memory". Every key was swept and every run
  ended on the same frame, which read as "no key dismisses it" — and the
  reading was wrong in a way worth keeping: **the title's answer to the key is
  to save and exit**, so the last frame of the run is the notice whether the
  key worked or not. A sweep that compares final frames cannot see a title that
  quits.

  What it actually does, read off the module and then watched live: the notice
  is screen 23 of the title's own screen table, chosen at one site by a word in
  its state block being zero. On the OK key its handler sets that word, writes
  a hundred bytes to `fs/nv.dat` and calls `MC_knlExit` — which is exactly what
  the notice asks for. **A second run against the tree the first one wrote goes
  straight past it**, into the usage notice, the mode select, the publisher
  screen and its loading bar, and stops where the other online titles do: on
  its own "the connection is delayed" dialog, and past that at its character
  naming screen, where WIPI C slot `0x321` is not implemented. So the notice is
  a first-run step rather than a wall.

  Two things came out of chasing it. **`Zio_Confirm` is a red herring**: the
  eight-byte record is read the way the earlier reading said, and nothing about
  the notice depends on it — no file has to be supplied to get past the screen.
  And the run after the notice found a real gap in the core: an ARMv6 `nop` in
  a Thumb-1 module, which is what a **patched archive** looks like when a
  cracker writes two bytes over a branch. See `docs/armcore.md`, "The hint
  space".

  The tools that settled it are worth naming, because reading the module alone
  had already been wrong twice: `-trace-live ''` showed the guest's own calls
  in the ticks after the key — `fsOpen("nv.dat", read-write)`, a write, then
  the exit — and the cheat console's `read` said which screen the title thought
  it was on, without stopping it.

  It reached the library by being filed under the wrong vendor — `app_info`
  inside, sitting in `var/games/ktf` — where the KTF acceptance test walks the
  directory rather than sniffing each archive, so it failed there as "no
  `__adf__`" and said nothing about itself. `detect.Archive` had it right all
  along. A test that walks a vendor directory is trusting the directory.

  **The naming screen is past, and the title plays its opening.** With slot
  `0x321` implemented and the component block refused — the title draws its own
  input screen — the run goes: the usage notice, the consent screen, the
  delayed-connection dialog, the title, the slot select, and a prompt asking
  whether to type the character's name. The screen that follows is a single
  bordered field with `OK:완료` and `CLR:삭제`, and **the OK key cycles the
  preset names rather than confirming immediately** — 빈스, 아론, 루크, 에반,
  세라, 숙자 — so a script that presses it once and expects the field to close
  reads as a screen that ignores its own prompt. Pressing on past the end of
  the list commits, and the title runs its opening: black-screen narration with
  `SKIP:#`, then a real map with the party, portraits and dialogue.

  **Free movement is reached, and the opening is skippable.** The narration
  prints `SKIP:#` and means it: a route that presses `#` and `fire` in turn,
  sixty ticks apart, walks the whole opening in about four thousand ticks and
  comes out in the title's own dungeon — party sprites, enemies, an HP/MP/EXP
  bar and a `콤보 타격 0/3` counter over it. `fire` there is the attack, and a
  **held** direction is what moves: `press down` / `release down` a few times
  scrolls the map and takes the party into the next room, where the HUD moves
  itself out of the way. So the title plays. What has not been driven past that
  is anything the party's own progress depends on — its save, its shops, its
  menus — and the route to any of them starts from the script above rather than
  from the notices again.

## A transparent pixel still has a colour

A guest surface is RGB565. There is no alpha channel in it, so when an encoding
declares some of its pixels transparent, that fact has to travel somewhere
else — and this platform sends it two ways at once, because two different
readers need it.

`framebufferFromImage` keeps an `opaque` mask beside the pixels, and every
draw path the platform owns consults it. That is the reader everyone thinks of.
**The other reader is the game.** A local title never asks the platform to blit
a sprite: over a run that reaches its character-select screen it makes 178
`MC_grpCreateImage` calls, 178 `MC_grpGetImageFrameBuffer` calls, **51,170**
calls for a framebuffer's raw pointer — and not one `MC_grpDrawImage`. It
decodes through the platform, takes the pointer, and runs its own blitter over
the words. All the platform contributes to a sprite is the pixels themselves.

For that reader the colour *is* the transparency. Every one of the 52 paletted
PNGs this title ships that declares a `tRNS` entry declares the same colour:
**pure magenta, `#ff00ff`** — `0xf81f` in RGB565, which the platform's own blit
already treats as the mask colour. The blitter skips the pixels that match it.

Go returns colours premultiplied. A pixel with zero alpha therefore comes back
from `RGBA()` as **black, whatever the artist put there**, and writing that into
the surface deletes the only transparency the guest can see. The blitter's key
then matches nothing and it paints the sprite's whole rectangle. On screen that
is a character standing on a solid box, a logo in a grey slab, a battle effect
in a black square — the same defect wherever a title blits for itself.

So the conversion reads the **non-premultiplied** form, `color.NRGBAModel`, and
the mask is recorded as before. `DecodeBitmap` was clearing its declared entry
to a zero `color.NRGBA` for the same reason and now clears only the alpha.

Two things worth keeping from how this was found:

- **The symptom was invisible at 1x.** Reading a captured 240x320 frame, the
  boxes did not register and the screen was called clean; the same frame put
  through the hqx filter showed them immediately. A capture that disagrees with
  a player's screenshot is worth magnifying before it is worth disbelieving.
- **The call histogram answered "whose bug is it" in one run.**
  `runlgt -trace-live ""` over a route to the screen, with the slots counted,
  showed the platform was never asked to draw a sprite. That ruled out every
  blit path at once and left exactly one thing the platform hands over.

### An LGT A/B is exact, but only when the runs are alone

Driving all twenty-eight local archives through one generic route under two
builds and comparing the captured frames is the regression net here, and unlike
the KTF sweep it has **no noise floor to read against** — a route replayed
against the same binary produces a byte-identical frame, because the guest
clock is virtual and a route counts ticks rather than seconds.

That exactness holds only for a run with the machine to itself. The sweep above
ran six archives at a time, and one title came back differing between the two
builds; re-run on its own, **the same binary disagreed with its own sweep
capture**, and the two builds agreed with each other. So the difference was the
load, not the change.

The rule that follows: a difference found in a parallel sweep is a candidate,
not a finding. Re-run that one archive alone under both builds before believing
it. Of eighty-four pairs, three differed and one was this.

### Two titles, two opposite ways of drawing

The fix above is worth nothing to the title that reports a similar symptom, and
counting platform calls is what says so. Over one route each:

| | title that blits for itself | title that lets the platform draw |
|---|---|---|
| `MC_grpDrawImage` | **0** | **188,587** |
| `MC_grpFillRect` | 0 | 239,871 |
| `MC_grpSetContext` | 6,644 | 498,685 |
| framebuffer pointer | 51,170 | 87,332 |

The second title's transparency already works, and not through its
declaration: it ships 2,282 BMPs, 2,244 of which declare a transparent entry
with an index of **256** against a 256-entry palette — out of range, so it is
ignored, exactly as the status-bar case above. It does not matter, because 63%
of those bitmaps are dominated by `#ff00ff` and `drawImage`'s fallback keys on
magenta anyway. Two independent routes to the same answer is why the symptom
never appeared there.

What is left for that title is the part of the context nothing reads.
`grpContextAlpha` and `grpContextPixelOp` are carried in the guest's structure
and handed back on request, and **no draw path consults either** — so anything
the title meant to blend arrives opaque. It sets field 5 85,830 times in one
route, and the values are colours (`0x2a75`, `0x23bd`), not operation codes,
which suggests the field numbering is off by one and 5 is `param1` — the colour
operand of a blend. That is a question for the specification rather than for
another guess.

### A screen that comes out grey, and the four platform answers it does not blame

One title's slot-selection screen draws its map background in about two dozen
blue-greys where the rest of the screen — the parchment panel, the sprites, the
text — is in full colour. It was reported as a "black and white screen" and had
never been reproduced; replaying the reported session's own key presses as a
route does not reach it, but four scripted key presses do (dismiss the
connection dialog, decline the authentication offer, then start), and the
screen it lands on matches the report pixel for pixel.

This is the title that blits for itself: it writes the framebuffer directly,
and over the whole route it makes **no** `MC_grpDrawImage` call at all. That
leaves the platform four ways to be at fault, and all four are measured clean:

| Platform answer | What the run says |
|---|---|
| framebuffer geometry | `MH_fbGetBpp` = 16, `MH_fbGetBpl` = 480 for a 240-wide screen |
| `MC_grpGetPixelFromRGB` | 28,145 calls, all `(r, g, b)` in 0–255, all converted to 5-6-5 |
| resource bytes | every `.gsa` and `.dat` opens and reads at its packaged size |
| allocation contents | filling every non-`calloc` block with `0xa5` changes nothing on screen, so nothing is drawn from memory the title never wrote |

The title also asks for **2,531 distinct colours**, 1,535 of them saturated, so
its palette is not the grey the screen shows. The pixels in that region are
therefore the title's own writes, from its own decode.

**Profiling the screen alone says which of its routines writes them.** A
route's `mark` restarts the profile, so marking the moment before the screen
appears gives the scene rather than the boot: 36.4% in `0xb810-0xb8be` and
27.0% in `0xe44e-0xe508`, the RLE sprite blitter this title's entry in the
table above already names. Disassembling the first one finds the only routine
in the run that could desaturate anything:

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

It is a blend whose strength comes from the source pixel's luminance, which is
exactly the shape a "dim what is behind the panel" effect has. **It is not what
makes this screen grey**: patching its first halfword to `bx lr` through the
cheat console — `set 0xb810 0x4770 u16`, which the decode cache honours because
every write drops it — leaves the screen the same. The grey is what the sprite
blitter drew, from the title's own asset, through its own palette.

So every path from this side is clean and the remaining question is not a
platform one: **what does this screen look like on a handset?** A photograph or
a capture from anywhere else settles in one look whether this is a stylised map
or a decode this platform is feeding wrongly, and nothing measurable here can.

## Guest profiling, and what it says about where the time goes

`runlgt <zip> -profile report.txt [-profile-folded stacks.txt] [-profile-from tick]`
samples the ARM core and ranks the guest code the title is running. The
sampler is `internal/armcore`'s and the ranking is
`internal/guestprofile`'s — both shared with KTF — and what is LGT's own is
only the question of what an address *is*.

**Executable addresses are deliberately left unnamed.** A Clet is a stripped
ARM executable with no method names, so the temptation is to fill the gap with
the ELF section, and that is worse than nothing: naming every address in the
module `.text` collapses the entire ranking into one row. The shared region
grouping only groups frames that carry no symbol, and a nameless image is
exactly what it was built for — so code comes back as the *address range of the
loop that is running*, which is what a disassembler is pointed at. What does
get named is everything that is not the title's code: the heap, the stack, the
platform's stub area, and the module's own data sections. A sample in the stub
area is a platform call in flight, and a bare hex number cannot say that.

### The guest is not spread out. It is one loop.

Five local titles, three hundred to four hundred ticks each:

| title | hottest regions | share | code size |
|---|---|---|---|
| A | `0x3a550-0x3a698` | **92.3%** | 328 B |
| B | `0x7084-0x7280` + `0x6d8e-0x6e5e` | **93.9%** | ~700 B |
| C | `0xe44e-0xe508` | **87.2%** | 186 B |
| D | three regions | **88.6%** | ~700 B |
| E | three regions | **80.4%** | ~1.1 KB |

Every one of them spends four fifths to fifteen sixteenths of its instructions
inside a kilobyte of code. Disassembling the top two settles what that code is,
and it is the same thing both times:

- B's 75% loop is an **RLE sprite blitter with per-channel blend tables**. The
  outer loop reads a 16-bit token — a terminator, a skip-to-next-row, a run of
  transparent pixels, or a run length — and the inner loop, per pixel, indexes
  a palette, pulls the 5-bit red, green and blue out of both the source and the
  destination pixel, looks each channel up in a 32x32 blend table chosen by a
  per-call level, and packs the result back.
- B's 18% loop is the same RLE stream with a **general mask-and-shift blend**:
  eight `(pixel & mask[i]) >> shift[i]` terms summed, both tables handed in.
- A's 92% loop is a different engine doing the same kind of work: a clipped
  rectangle **alpha blend** that memcpys a row into a stack buffer, blends each
  channel by two scalar weights, and copies it back. It is the fade — the same
  title sets the context's alpha 619 times a run.

Two things follow, and they point in opposite directions.

**The addresses differ per title, so there is no one routine to recognise.**
These are the games' own engines, not a shared library, and a hook would be a
byte signature per engine the way `MC_knlAlloc`'s callers are not.

**But the working set is a kilobyte.** That is the best possible news for
translation: whatever a block translator costs to build a block, it is paid
once and amortised across millions of executions, and the hit rate is not a
question at all. It also means the dispatch overhead the host profile measures
— `Engine.Run`'s own 24% plus the decode cache's 9% — is being paid on *this*
loop, every instruction of it, which is precisely the code a translator would
stop paying it for.

**What that leaves a player with, measured on the title the question was asked
about.** A tick here is 50ms of guest time, so the number that decides whether
a title is comfortable is host milliseconds per tick against that. Driven from
a cold start through its notices, its authentication failure, its character
select and into the opening boss scene — chained lightning pillars, particles,
a full-screen animated boss — the release build spends **7.3ms a tick over the
first 3,000 and 9.0ms a tick over the next 3,000**, which is the in-game half.
That is **five to six times faster than the guest clock it is driving**, and the
whole 6,000-tick run costs 49s of host time for 300s of guest time. Its boot
retires instructions at **6.8ns each**.

**The one number that is not comfortable is a single tick**: the scene load
after character select takes 1.27s in one tick, and it is the same tick in every
run. That is the shape a load has here — a whole scene arrives inside one tick —
and it is why the run summary reports the slowest tick beside the total. An
average would hide it completely, and it is the only thing in this run a player
would notice.

### A Java title is the other shape, and it is the slow one

The five above are Clets. Profiling a **Java** title over a replay of a
reported session — 5,915 ticks, driven by the session's own key presses — puts
the same question a different way:

| | share | what it is |
|---|---|---|
| `0x9ba3c-0x9bd1c` | 36.3% | the title's own |
| `0xe62c-0xe6c0` | 14.1% | ” |
| `0xe1f18-0xe1fac` | 13.5% | ” |
| `[platform stubs]` | 11.8% | the Java call thunks |
| `0xe2194-0xe2390` | 8.6% | the title's own |

Three quarters is still the title's own code, so the working-set argument holds
and there is no platform call to blame. What is new is the **11.8% in the
stubs**: a Java title reaches the platform through a thunk per call, and that
is a cost a Clet does not pay at all.

The run costs 150.8s of host time for 295.8s of guest time — 25.5ms a tick
against the 50ms a tick stands for — so it is twice as fast as the clock rather
than the five to six times a Clet manages, and a reported session at 10fps sits
inside that gap. The same run on a debug build costs 167.1s, which is **10.8%**
of it: a player who wants the frames back should be on the release build first.

#### That profile was of the guest, and the host was doing something else

Every number above is a **guest** profile: it says which guest instructions
were executed, and it was read as if it also said where the host's time went.
It does not, and taking a host profile of the same run says so plainly:

| | share of host time |
|---|---|
| `syncToGuest` | 29.0% |
| `syncFromGuest` | 26.3% |
| the scheduler parking and waking guest threads | ~17% |
| **the whole ARM interpreter** | **4.2%** |

The interpreter was never the problem. **Two thirds of the host's time was
copying the framebuffer in and out of guest memory around every drawing call**
— see `javaDraw` — and the fix is in "A Java title's drawing does not
synchronise, and a Clet's has to" below. After it, the same route on the same
build costs **16.9s instead of 150.9s**, with the same 411,577,534 guest
instructions retired and all 2,484 frames byte-identical: 41.0ns a step rather
than 366.6, and 17.5 times the guest clock rather than 1.96. A tick went from
25.5ms to 2.9ms.

The lesson is worth more than the number. A guest profile answers "what is the
title computing"; only a host profile answers "what is this emulator spending
its time on", and the two had been read as the same question for as long as
this section has existed. `docs/armcore.md` carries the same correction from
the other side.

## Finding out what a title was told

A fault here is almost never at the instruction that reports it. A slot answers
zero or an error code, the game stores that word, and the dereference happens
hundreds of instructions later in code that has no idea a platform call was
involved. The fault address names the game, not the cause.

So the platform records its recent calls. `Options.TraceSVC` / 
`SessionOptions.TraceSVC` set the size of a ring buffer of the last N calls,
and `Client.SVCTrace` / `Session.SVCTrace` read it back; `FormatSVCTrace`
renders it oldest first. From the CLI:

```sh
wfeature runlgt <game.zip> -ticks 20 -trace 25
```

The dump appears on any failed start or tick, and at the end of a run that did
not fail — a title that reached a screen it should not have is being answered
wrongly somewhere, and the ring is the only record of what it asked. Each line
carries the category and slot, the slot's name or `unnamed`, `r0`-`r3` on entry,
the result or the error, and **the link register** — the address in the game's
own code that made the call, which is what turns a trace into a place to look.

A startup fails before `StartSession` can return a session, which is exactly
the run worth reading, so the trace travels out on a `StartFailure` error
rather than dying with the client.

`unnamed` is a finding rather than a formatting gap: it marks a slot this
platform is answering without knowing what it is.

**The Java slots are named too, and they have to be named from the loaded
module.** A static entry and a vtable slot are numbers until the class tables
the module handed over say what they stand for, so the naming is a method on the
client rather than a table lookup — `javaSlotName`, the compact form of what a
failure at one of those slots reports. Without it a Java title's whole trace
reads `unnamed`, every line of it, because a Java title makes almost no C calls;
and those are exactly the titles whose remaining faults are all on the Java
side. With it, a screen that will not advance becomes a list of what the title
was asking for while it would not — which is how the wait below was read:

```
java 0x2000030 java/lang/Thread.<class>(...) from 0xd7124
java 0x2000034 java/lang/Thread.sleep(J)V(0xa, ...) from 0xd7138
java 0x55 leave a method(...) from 0xd7118
```

Three lines, repeating, for the rest of the run. The names come from
`javaSVCNames`, the same table a failure reports through, rather than a second
one beside it.

### Where a title is waiting, in the title's own names

Three lines repeating say a title is waiting; they do not say **which of the
title's own methods is waiting**, and in a megabyte of compiled Java that is
the whole question. Two things answer it, and both are on by default in a debug
build.

**Every serviced Java method call carries the caller's address.** The debug
line for a platform method now ends `from=0x...` — the link register, an
address in the module's own code. Grouping a run's calls by it is a profile of
which of the title's methods are doing anything at all, and the count alone
separates a game loop from a screen that is stuck:

```sh
wfeature runlgt <game.zip> ... 2>calls.txt
grep -o 'from=0x[0-9a-f]*' calls.txt | sort | uniq -c | sort -rn | head
```

**And a thread that sleeps in one place for long enough reports its stack.**
The compiled code keeps an APCS frame — `mov ip, sp; push {…, fp, ip, lr, pc};
sub fp, ip, #4` — so `fp` points at the saved `pc` and the three words below it
are the return address, the caller's stack and the caller's frame. Walking that
chain and naming each address by the nearest method body in the class records
gives the title's own call stack, which `javaBacktrace` does. Inside a platform
call the program counter is a stub, so the innermost frame is named by the link
register instead.

After 500 sleeps from one call site — five seconds of guest time at the ten
milliseconds a spin loop sleeps for — the wait is reported once:

```
msg="LGT java thread is waiting in one place" sleeps=500
    stack="w.i()V+0xf0 < b.run()V+0x208 < 0x7fff0000"
```

That line is the save-backup screen above, and it took one run where reading the
member tables and disassembling took several: it names the waiting method, and
it names the Jlet's own loop as what called it, which is what turned "a card
that is never pushed" into "a screen that is waiting to be painted". The same
stack goes into the failure a platform method reports, so an unimplemented call
now says which of the title's methods wanted it.

#### What that wait is actually testing

Disassembling the named method around the sleep gives the predicate, which is
worth writing down because it says what would have to happen for the screen to
move rather than what is missing:

```
ldr   r2, [fp, #-0x40]      ; a local the method already held
ldr   r3, [r2, #8]          ; r3 = that object's field at +8 — an array
ldr   r2, =0x01500a30
ldrsh r2, [r2, #0x44]       ; a signed halfword global: the index
ldr   ip, [r3, r2, lsl #2]  ; array[index]
cmp   ip, #0
bne   … sleep(10) and test again
```

A first reading took that for `while (slots[index] != null)` — an object array
indexed by a global short — and it was wrong in the way that matters, because
it sent the next step at a watchpoint on an address that does not exist.
**`0x01500a30` is not a variable. It is the field-index out array**, the
seventh argument of the load call (`0x14`), which this platform fills in at
startup. So `ldrsh r2, [r2, #0x44]` reads a *constant for the run* — the word
index of field-table **entry 34** — and `[r3, r2, lsl #2]` is one field of one
object, not one element of an array.

Three debug lines turn that into names, and each was added for this:

- `LGT java platform class` now lists the **field table** entry by entry.
  Entry 34 is `d:Z`, a boolean.
- `LGT java class laid out` now carries `entries=first..last`, which class
  answered which stretch of that table. 34 falls in `w`'s 29..38, so the field
  is `w.d`.
- `LGT java class` now prints each method body's address beside its name, which
  is what names an address the other direction. `w.i()V@0xd7048` puts the
  sleeping `+0xf0` at `0xd7138`, exactly where the trace said.

So the predicate is `while (this.d) sleep(10)` on a boolean — and a boolean has
writers, which a scan finds. Every access to entry 34 is an `ldrsh` of `#0x44`
against a base loaded from the literal pool, and there are **four in the whole
module**:

| Site | In | What it does |
|---|---|---|
| `0xd3078` | `w.<init>()V` | sets `d = 1` |
| `0xd7eec` | `w.l()V` | sets `d = 1` |
| `0xd7148` | `w.i()V` | the wait above |
| `0xd6f30` | `w.b(Lorg/kwis/msp/lcdui/Graphics;)V` | **clears it** — `d = 0` |

One writer clears it, and `w.b(Graphics)` is the method that draws the card's
own content: it dispatches virtual entry 71, `a(Lorg/kwis/msp/lcdui/Graphics;)V`
— the override each card class carries — and clears the flag on the way out.
`d` is "this card has not drawn yet", and `i()` is the wait for it to have
drawn.

**`w.b(Graphics)` is reached from exactly one place**, and it is not a vtable:
`w.paint`'s literal pool holds its address at `0xd6ca8` and calls it at
`0xd6c44`, behind a test of one of the class's own statics. In a stuck session
`w.paint` runs — the debug build's `from=` addresses land inside it — and the
guarded call never does. **The title is waiting for a card that the platform
is painting**, and paint is taking its other branch every time.

#### The gate, and what holds it open

The static `w.paint` tests at `0xd6aec` is one of the class's own, at
`data + 0xd4`, and the same scan finds its two writers among the class's
methods: `w.f()V` sets it to 1 and bumps a counter beside it, `w.g()V` sets
both back to 0. A notice is up, and paint draws the notice instead of the card
while it is. `f` and `g` are virtual entries 63 and 61, and the dispatch scan
gives their call sites: **`g` has two in the whole module**, and one of them is
in `w.k(I)I` — the save-backup routine, the method holding the `Network` calls.

`w.k(I)I` is where it comes apart:

```
ldr ip, [r7, #0x84] ; mov lr, pc ; bx ip   ; Network.connect()
cmn r0, #1                                 ; did it answer -1?
bne  0xd8384                               ; no: dial the socket
mov  r0, r8 ; …call…                       ; yes: one call
mov  r0, #2
b    0xd8e28                               ; the epilogue — past its own g()
```

**The failure branch jumps past the teardown.** The notice stays up, paint keeps
drawing it, `w.d` is never cleared, and the Jlet's loop parks in `w.i()`
for ever. No platform call is wrong and nothing reports anything.

Two experiments say so, both with the cheat console patching one word of guest
code in a running session — which is what that console turns out to be good for
beside cheats:

- **Force the gate.** `set 0xd6af4 0xea00004e` makes `paint` always call
  `w.b(Graphics)`. The wait report disappears, flushes go from 528 to 1351, and
  the title paints its way to its own title menu.
- **Force the connect test.** `set 0xd8368 0xea000005` makes `k` take the
  branch it would have taken had the network answered. It dials
  `URL.find("socket://…")` — which used to stop the session as an unimplemented
  slot and now answers the specification's `SchemeNotFoundException` — the
  title **catches it**, takes its own notice down, and reaches the same menu.

So this title is not blocked on anything the platform gets wrong; it is blocked
on `Network.connect` answering `-1`, which is what the specification says a
handset with no access answers. [`network.md`](network.md) carries that
decision and why it was not changed on one title's evidence.

What is worth keeping past this title is the recipe, because an out-array
offset in a disassembly is the commonest dead end this ABI produces:

1. read the offset and the base out of the instruction;
2. match the base against the load call's arguments to say *which* table it is
   (`0x1500a30` fields, `0x150086c` static fields, `0x15007b4` virtual methods,
   `0x150086e` methods, `0x1500874` static methods — this title's, and the
   addresses are the module's own);
3. halve the offset for the entry, and take the name off the debug lines above;
4. scan `.text` for every other `ldrsh` of that offset against that base. The
   instruction after each says load or store, which sorts readers from writers
   in one pass.

## Playing a title from the command line

The trace says what a title was told; these say what it did with it.

```sh
wfeature runlgt <game.zip> -ticks 1400 -framedir out -save dir \
    -key 30:fire -key 760:fire -hold 30 -steps 600000000
```

- `-key <tick>:<name>` presses a key on that tick and releases it `-hold` ticks
  later. The release is scheduled rather than queued behind the press, because a
  game that samples the keypad once a frame never sees both in one tick. The
  names are the shared WIPI ones (`up`, `fire`, `soft1`, digits, `*`, `#`).
  **Raise `-hold` before believing a screen refuses a key.** One title's
  character select answers `right` at a one-tick hold and ignores `OK` entirely;
  at twenty it selects, loads and drops into the world. A screen that answers
  one key and not another is the shape that says to try this first, because a
  key the title has no code for and a key it never saw look identical.
- `-framedir` writes a PNG per presented frame, which is what makes a run
  reviewable: a title's whole boot, menu and first minutes read as a contact
  sheet.
- `-steps` overrides the per-call instruction budget, which is how the world
  load was measured before the default was raised.
- `-audio out` records what the run played, `out.mid` for the MIDI events and
  `out.wav` for the sampled ones, which is how a sound path is checked without
  a speaker in the loop.

The flags are in [`cli.md`](cli.md); what to do with them — driving a title to
a scene, probing a screen that will not advance, diffing two builds frame by
frame, and reading a filtered call trace — is the third layer of **Testing**
below, because that is what it is.

## Testing

There are three layers, and each exists because the one below it is blind to a
whole class of defect. None of them subsumes the others.

### 1. The fixture and the slot tests — `go test ./internal/platform/lgt/`

The fixture is hand-assembled ARM in `fixture_test.go`, with a small literal-
pool assembler so the program reads as assembly rather than as hex. Its body is
ARM, so it opens with the `bx pc` trampoline a mixed module uses to get there
from the Thumb entry, and its calls out are the ARMv4T `mov lr, pc; bx rN`
sequence rather than `BLX`.

Beside it are slot-level tests that drive one WIPI C call at a time against a
loaded client, which is how a contract like the resource pair gets pinned down
without assembling a module that calls it.

The Java pieces are tested this way too — the try/throw stack, the text
objects, the resource streams, `arraycopy`'s element widths, and the class
object's static run — because each is a contract that can be driven without a
module.

**What this layer cannot see: a contract that is wrong in the same way the
fixture is.** Every LGT contract that turned out to be wrong looked correct
here, because the fixture was written to the same misunderstanding — a resource
read answering a length where zero meant success, a timer taking its callback
from the arming call's parameter, a graphics context this platform thought it
owned. Being ARM where a real module is Thumb is the sharper form of the same
limit, and it is why five instruction-set faults survived until a real archive
ran: the Thumb entry, ARM `BLX` immediate, `BLX` register, **Thumb `BLX`
immediate**, and **CP15 cache maintenance**. A change to the entry path is worth
checking against `var/games/lgt` by hand.

### 2. The acceptance probe — does every real module still boot and paint

```sh
WFEATURE_LGT_ACCEPTANCE=1 go test -run TestLocalLGTArchivesBootAndPaint -v ./internal/platform/lgt
```

Every archive under `var/games/lgt` is started, ticked, and required to present
a frame with something lit in it; a Java app skips with a named reason. It is
the only test in the package that runs a real module, and it is what keeps the
corrected contracts from drifting back. Nine seconds for twelve titles, so it
is cheap enough to run on any change to the platform surface.

**What this layer cannot see: anything past the title screen.** Both defects
found in the pass that added this section — every line of a title's dialogue
rendering as `%.*s`, and two titles losing their whole opening sequence to a
delete that did not delete — sailed through it. Each title booted, painted,
reached the world and saved. The screens were simply wrong.

### 3. The scripted run — what the title does with what it was told

This is the layer that catches gameplay defects, and it is a method rather than
a test, because what a game is supposed to do next is not something the
repository knows.

**Drive it and look at every frame.** `-key` and `-framedir` produce the frames;
`contactsheet` makes them one page. A title's whole boot, menu, character
creation and first minutes read at a glance, which is how a scene that never
plays becomes visible at all — its absence is not something a single frame
shows:

```sh
wfeature runlgt <game.zip> -ticks 2000 -framedir out -save /tmp/probe \
    -key 40:fire -key 330:down -key 390:fire -key 450:fire
wfeature contactsheet out sheet.png -every 18
```

Give every probe run its own `-save`, and never omit it. Without it the run
writes into the real save tree, and the run after it starts from a different
place — which reads as "that key does nothing" and is the wrong conclusion.

**A screen that will not advance is worth probing rather than guessing at.** Run
the same key prefix once per candidate key and compare the last frame — a
one-line loop over `fire soft1 soft2 clear call hangup` and the arrows and
digits, `-frame` each, and compare the hashes. Two answers are both findings:
one key differing names the key, and *every key identical* says the screen is
not waiting for one, which is what sent the restart-notice title above to its
filesystem trace instead.

That is how the confirm key on a character-creation screen was found — it was
the obvious one, and the run had simply been ending before the load it started
finished. It is also how two Java titles that had been recorded as stopped
turned out to be sitting on a dialog with a confirm softkey. **A title that is
waiting for a key looks exactly like a title that has stopped**, and the sweep
costs one run per key. A run that seems stuck is as often too short, or too
quiet, as it is wrong.

**Diff two builds frame by frame.** Run the same key script against the binary
before a change and the binary after it, and `framediff` the two directories:

```sh
wfeature framediff after/ before/
  tick1023  (67,262)-(76,272)
  ...
4937 of 5798 frames present in both runs differ
```

The first differing tick names what the change actually did and the bounding
box names where on the screen — `(67,262)-(237,293)` is the dialogue box and
nothing else. This turns "I think I fixed it" into a tick and a rectangle. It
is also what kept a wrong conclusion out of the dialogue fix: the screen the
defect was first reported on came back **byte-identical** between the two
builds, so it was not the screen, and the real one was three hundred ticks
further in.

**Ask what the title was told, filtered.** `-trace-live` streams platform calls
as they are serviced, with the arguments that are names read as names. Filtering
to a slot family is what makes it readable, and a wrong answer usually reads as
an obvious wrong answer once the story is in order:

```sh
wfeature runlgt <game.zip> -ticks 600 -save /tmp/probe -trace-live fs ... 2>calls.txt
```

The whole delete defect was two adjacent lines of that output. Reach for it
before reaching for the disassembler: it needs no static analysis, and it says
what the title asked for rather than what it might have asked for.

**Static analysis when the trace is not enough.** `python3` with `capstone`
disassembles Thumb; the module's lazy-import stub table decodes to the complete
set of slots a title uses, without running it. Do not name the script `dis.py`
— it shadows the standard library module `capstone` imports.

Two scans pay for themselves whenever a Java slot is the question:

- **Every dispatch of one vtable slot.** A dispatch is four words —
  `ldr rB, [recv]; ldr ip, [rB, #(slot+1)*4]; mov lr, pc; bx ip` — so scanning
  `.text` for the `ldr ip` encoding with the call sequence behind it lists the
  sites of a slot across the whole module, which is what turns "one call site is
  not enough" into a choice. Two things keep it honest: a base register loaded
  from a **literal** is an import table and not a vtable at all, and only the
  *nearest* write of that register counts — taking any earlier `ldr rB, [recv]`
  turned six real sites into sixteen.
- **Every use of one static field.** The field-index array the platform fills in
  is read as `ldrsh rX, [base, #entry]` against a base loaded from the literal
  pool, so the same literal-pool index gives every site that touches one field,
  and the instruction after each says whether it loads or stores. That is how
  the flag a stuck title polls was traced to the one routine that clears it.

**A watchpoint when the question is "who wrote this".** `Core.Watch` records
every guest store to an address with the instruction that made it. Three passes
of reasoning about who could have overwritten a class object ruled out three
plausible causes and found none of them; one run with a watch on the word named
the instruction, and the answer followed from disassembling it. Reach for it
first, not last.

## Cheats

`Session.Cheat()` is the same engine KTF uses, over this platform's address
space. Regions are labeled `module` / `heap` / `platform` / `stubs` / `stack`;
unlike KTF the module is not at a fixed base — an ELF keeps whatever addresses
it names — so the module's span is asked of the loaded module rather than
compared against a constant.

`Session.Tick` rewrites frozen values after the game has painted, so a cheat
wins over whatever the tick just wrote. The CLI attaches the text console with
`wfeature runlgt <game.zip> -cheat`, which paces the run to about real time so
typed commands get a turn between ticks.

The browser reaches the same engine, and did not for a while although
everything behind it was built. The server asked the platform-specific
`Session.KTF()` for an engine and answered "the cheat engine is only wired to
KTF" when that came back nil, so a panel over an LGT game refused every
operation while the engine it was refusing on behalf of was attached and
running. Hosts now ask `session.Cheat()` and `session.CheatConsole()`, which
answer for whichever platform is behind the session and nil where none does.
That indirection paid for itself twice: the MIDP runtime, which had no address
space to sweep and answered nil, later grew a synthetic one and needed two lines
here to reach the same panel (`docs/skvm.md`). Reaching for a platform by name is
what hid this: the refusal message named the platform it had asked for rather
than the property it needed, so it read as a decision instead of a gap.
